from collections.abc import AsyncGenerator, AsyncIterator, Callable
from contextlib import asynccontextmanager

import grpc
import grpc.aio
import pytest

from libre.hub.v1 import guilds_pb2, guilds_pb2_grpc, stats_pb2, stats_pb2_grpc
from libre_hub_client import KEEPALIVE_OPTIONS, connect


class StatsServicer(stats_pb2_grpc.StatsServiceServicer):
    def __init__(self) -> None:
        self.seen: dict[str, str | bytes] = {}

    async def GetStats(
        self,
        request: stats_pb2.GetStatsRequest,
        context: grpc.aio.ServicerContext[stats_pb2.GetStatsRequest, stats_pb2.GetStatsResponse],
    ) -> stats_pb2.GetStatsResponse:
        self.seen = dict(context.invocation_metadata() or ())
        return stats_pb2.GetStatsResponse()


class GuildServicer(guilds_pb2_grpc.GuildServiceServicer):
    def __init__(self) -> None:
        self.seen: dict[str, str | bytes] = {}

    async def WatchGuildSettings(
        self,
        request: guilds_pb2.WatchGuildSettingsRequest,
        context: grpc.aio.ServicerContext[guilds_pb2.WatchGuildSettingsRequest, guilds_pb2.WatchGuildSettingsResponse],
    ) -> AsyncIterator[guilds_pb2.WatchGuildSettingsResponse]:
        self.seen = dict(context.invocation_metadata() or ())
        yield guilds_pb2.WatchGuildSettingsResponse()


@asynccontextmanager
async def serving(register: Callable[[grpc.aio.Server], None]) -> AsyncGenerator[str]:
    server = grpc.aio.server()
    register(server)
    port = server.add_insecure_port("127.0.0.1:0")
    await server.start()
    try:
        yield f"127.0.0.1:{port}"
    finally:
        await server.stop(0)


async def test_connect_sends_bearer_token() -> None:
    servicer = StatsServicer()
    async with serving(lambda s: stats_pb2_grpc.add_StatsServiceServicer_to_server(servicer, s)) as address:
        async with connect(address, "t", tls=False) as channel:
            await stats_pb2_grpc.StatsServiceStub(channel).GetStats(stats_pb2.GetStatsRequest())
    assert servicer.seen["authorization"] == "Bearer t"


async def test_connect_sends_token_on_streams() -> None:
    servicer = GuildServicer()  # pyright: ignore[reportAbstractUsage]
    async with serving(lambda s: guilds_pb2_grpc.add_GuildServiceServicer_to_server(servicer, s)) as address:
        async with connect(address, "t", tls=False) as channel:
            stub = guilds_pb2_grpc.GuildServiceStub(channel)
            async for _ in stub.WatchGuildSettings(guilds_pb2.WatchGuildSettingsRequest()):
                pass
    assert servicer.seen["authorization"] == "Bearer t"


async def test_connect_keeps_caller_metadata() -> None:
    servicer = StatsServicer()
    async with serving(lambda s: stats_pb2_grpc.add_StatsServiceServicer_to_server(servicer, s)) as address:
        async with connect(address, "t", tls=False) as channel:
            await stats_pb2_grpc.StatsServiceStub(channel).GetStats(
                stats_pb2.GetStatsRequest(), metadata=(("x-test", "1"),)
            )
    assert servicer.seen["x-test"] == "1"
    assert servicer.seen["authorization"] == "Bearer t"


async def test_connect_sends_shards() -> None:
    servicer = StatsServicer()
    async with serving(lambda s: stats_pb2_grpc.add_StatsServiceServicer_to_server(servicer, s)) as address:
        async with connect(address, "t", tls=False, shards=lambda: ([1, 0, 1], 4)) as channel:
            await stats_pb2_grpc.StatsServiceStub(channel).GetStats(stats_pb2.GetStatsRequest())
    assert servicer.seen["libre-shards"] == "0,1/4"
    assert servicer.seen["authorization"] == "Bearer t"


async def test_connect_sends_shards_on_streams() -> None:
    servicer = GuildServicer()  # pyright: ignore[reportAbstractUsage]
    async with serving(lambda s: guilds_pb2_grpc.add_GuildServiceServicer_to_server(servicer, s)) as address:
        async with connect(address, "t", tls=False, shards=lambda: ([1, 0, 1], 4)) as channel:
            stub = guilds_pb2_grpc.GuildServiceStub(channel)
            async for _ in stub.WatchGuildSettings(guilds_pb2.WatchGuildSettingsRequest()):
                pass
    assert servicer.seen["libre-shards"] == "0,1/4"
    assert servicer.seen["authorization"] == "Bearer t"


async def test_connect_sends_no_shards_by_default() -> None:
    servicer = StatsServicer()
    async with serving(lambda s: stats_pb2_grpc.add_StatsServiceServicer_to_server(servicer, s)) as address:
        async with connect(address, "t", tls=False) as channel:
            await stats_pb2_grpc.StatsServiceStub(channel).GetStats(stats_pb2.GetStatsRequest())
    assert "libre-shards" not in servicer.seen


async def test_connect_sends_no_shards_when_the_callable_returns_none() -> None:
    servicer = StatsServicer()
    async with serving(lambda s: stats_pb2_grpc.add_StatsServiceServicer_to_server(servicer, s)) as address:
        async with connect(address, "t", tls=False, shards=lambda: None) as channel:
            await stats_pb2_grpc.StatsServiceStub(channel).GetStats(stats_pb2.GetStatsRequest())
    assert "libre-shards" not in servicer.seen


async def test_connect_asks_for_shards_on_every_call() -> None:
    servicer = StatsServicer()
    current: tuple[list[int], int] | None = None
    async with serving(lambda s: stats_pb2_grpc.add_StatsServiceServicer_to_server(servicer, s)) as address:
        async with connect(address, "t", tls=False, shards=lambda: current) as channel:
            stub = stats_pb2_grpc.StatsServiceStub(channel)
            await stub.GetStats(stats_pb2.GetStatsRequest())
            assert "libre-shards" not in servicer.seen
            current = ([2, 3], 4)
            await stub.GetStats(stats_pb2.GetStatsRequest())
    assert servicer.seen["libre-shards"] == "2,3/4"


def test_connect_requires_an_explicit_transport() -> None:
    with pytest.raises(TypeError):
        connect("127.0.0.1:1", "t")  # pyright: ignore[reportCallIssue]


def test_keepalive_options() -> None:
    assert dict(KEEPALIVE_OPTIONS) == {
        "grpc.keepalive_time_ms": 30000,
        "grpc.keepalive_timeout_ms": 10000,
        "grpc.keepalive_permit_without_calls": 1,
        "grpc.http2.max_pings_without_data": 0,
    }
