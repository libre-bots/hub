import grpc
import grpc.aio
import pytest
from google.protobuf.any_pb2 import Any
from google.rpc import code_pb2, error_details_pb2, status_pb2  # pyright: ignore[reportMissingTypeStubs]

from librebots.hub import connect, reason_of
from librebots.hub.v1 import stats_pb2, stats_pb2_grpc


class FailingStats(stats_pb2_grpc.StatsServiceServicer):
    def __init__(self, details: list[Any] | None) -> None:
        self.details = details

    async def GetStats(
        self,
        request: stats_pb2.GetStatsRequest,
        context: grpc.aio.ServicerContext[stats_pb2.GetStatsRequest, stats_pb2.GetStatsResponse],
    ) -> stats_pb2.GetStatsResponse:
        if self.details is not None:
            status = status_pb2.Status(code=code_pb2.NOT_FOUND, message="failed", details=self.details)
            context.set_trailing_metadata((("grpc-status-details-bin", status.SerializeToString()),))
        await context.abort(grpc.StatusCode.NOT_FOUND, "failed")
        raise AssertionError("unreachable")


def info(reason: str, domain: str) -> Any:
    packed = Any()
    packed.Pack(error_details_pb2.ErrorInfo(reason=reason, domain=domain))
    return packed


async def fail_with(details: list[Any] | None) -> grpc.aio.AioRpcError:
    server = grpc.aio.server()
    stats_pb2_grpc.add_StatsServiceServicer_to_server(FailingStats(details), server)
    port = server.add_insecure_port("127.0.0.1:0")
    await server.start()
    try:
        async with connect(f"127.0.0.1:{port}", "t", tls=False) as channel:
            with pytest.raises(grpc.aio.AioRpcError) as raised:
                await stats_pb2_grpc.StatsServiceStub(channel).GetStats(stats_pb2.GetStatsRequest())
    finally:
        await server.stop(0)
    return raised.value


async def test_reason_of_reads_error_info() -> None:
    error = await fail_with([info("REASON_GUILD_NOT_REGISTERED", "hub.libre-bots")])
    assert reason_of(error) == "REASON_GUILD_NOT_REGISTERED"


async def test_reason_of_ignores_other_domains() -> None:
    error = await fail_with([info("REASON_GUILD_NOT_REGISTERED", "other.example")])
    assert reason_of(error) is None


async def test_reason_of_without_details() -> None:
    assert reason_of(await fail_with(None)) is None


async def test_reason_of_ignores_unknown_details() -> None:
    unknown = Any(type_url="type.googleapis.com/unknown.Thing", value=b"x")
    assert reason_of(await fail_with([unknown])) is None


async def test_reason_of_ignores_corrupt_details() -> None:
    corrupt = Any(type_url="type.googleapis.com/google.rpc.ErrorInfo", value=b"\xff\xff\xff")
    assert reason_of(await fail_with([corrupt])) is None


async def test_reason_of_skips_corrupt_detail_before_valid_one() -> None:
    corrupt = Any(type_url="type.googleapis.com/google.rpc.ErrorInfo", value=b"\xff\xff\xff")
    error = await fail_with([corrupt, info("REASON_GUILD_NOT_REGISTERED", "hub.libre-bots")])
    assert reason_of(error) == "REASON_GUILD_NOT_REGISTERED"
