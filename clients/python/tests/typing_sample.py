import grpc.aio

from librebots.hub.v1 import stats_pb2, stats_pb2_grpc


async def total_guilds(channel: grpc.aio.Channel) -> int:
    stub = stats_pb2_grpc.StatsServiceStub(channel)
    response = await stub.GetStats(stats_pb2.GetStatsRequest())
    return response.total.guild_count
