from collections.abc import AsyncGenerator, AsyncIterator, Callable
from contextlib import asynccontextmanager

import grpc
import grpc.aio

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
        async with connect(address, "t") as channel:
            await stats_pb2_grpc.StatsServiceStub(channel).GetStats(stats_pb2.GetStatsRequest())
    assert servicer.seen["authorization"] == "Bearer t"


async def test_connect_sends_token_on_streams() -> None:
    servicer = GuildServicer()  # pyright: ignore[reportAbstractUsage]
    async with serving(lambda s: guilds_pb2_grpc.add_GuildServiceServicer_to_server(servicer, s)) as address:
        async with connect(address, "t") as channel:
            stub = guilds_pb2_grpc.GuildServiceStub(channel)
            async for _ in stub.WatchGuildSettings(guilds_pb2.WatchGuildSettingsRequest()):
                pass
    assert servicer.seen["authorization"] == "Bearer t"


async def test_connect_keeps_caller_metadata() -> None:
    servicer = StatsServicer()
    async with serving(lambda s: stats_pb2_grpc.add_StatsServiceServicer_to_server(servicer, s)) as address:
        async with connect(address, "t") as channel:
            await stats_pb2_grpc.StatsServiceStub(channel).GetStats(
                stats_pb2.GetStatsRequest(), metadata=(("x-test", "1"),)
            )
    assert servicer.seen["x-test"] == "1"
    assert servicer.seen["authorization"] == "Bearer t"


def test_keepalive_options() -> None:
    assert dict(KEEPALIVE_OPTIONS) == {
        "grpc.keepalive_time_ms": 30000,
        "grpc.keepalive_timeout_ms": 10000,
        "grpc.keepalive_permit_without_calls": 1,
        "grpc.http2.max_pings_without_data": 0,
    }
