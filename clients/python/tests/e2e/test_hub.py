import os

import grpc
import grpc.aio
import pytest

from libre.hub.v1 import guilds_pb2, guilds_pb2_grpc, stats_pb2, stats_pb2_grpc
from libre_hub_client import connect, reason_of

pytestmark = pytest.mark.e2e

ADDRESS = os.environ.get("HUB_E2E_ADDRESS", "localhost:50051")
TOKEN = "dev-token"


async def test_join_and_read_settings() -> None:
    async with connect(ADDRESS, TOKEN) as channel:
        stub = guilds_pb2_grpc.GuildServiceStub(channel)
        joined = await stub.GuildJoined(
            guilds_pb2.GuildJoinedRequest(guild_id=8388608, preferred_locale="de", member_count=42)
        )
        assert joined.settings.locale == "de"
        read = await stub.GetGuildSettings(guilds_pb2.GetGuildSettingsRequest(guild_id=8388608))
        assert read.settings.locale == "de"


async def test_stats_list_the_bot() -> None:
    async with connect(ADDRESS, TOKEN) as channel:
        stub = guilds_pb2_grpc.GuildServiceStub(channel)
        await stub.GuildJoined(guilds_pb2.GuildJoinedRequest(guild_id=8388608, preferred_locale="de", member_count=42))
        stats = await stats_pb2_grpc.StatsServiceStub(channel).GetStats(stats_pb2.GetStatsRequest())
    figures = {bot.bot: bot.figures for bot in stats.bots}
    assert "example" in figures
    assert figures["example"].guild_count >= 1


async def test_reason_crosses_the_wire() -> None:
    async with connect(ADDRESS, TOKEN) as channel:
        stub = guilds_pb2_grpc.GuildServiceStub(channel)
        with pytest.raises(grpc.aio.AioRpcError) as raised:
            await stub.GetGuildSettings(guilds_pb2.GetGuildSettingsRequest(guild_id=12582912))
    assert raised.value.code() == grpc.StatusCode.NOT_FOUND
    assert reason_of(raised.value) == "REASON_GUILD_NOT_REGISTERED"


async def test_wrong_token() -> None:
    async with connect(ADDRESS, "nope") as channel:
        with pytest.raises(grpc.aio.AioRpcError) as raised:
            await stats_pb2_grpc.StatsServiceStub(channel).GetStats(stats_pb2.GetStatsRequest())
    assert raised.value.code() == grpc.StatusCode.UNAUTHENTICATED
