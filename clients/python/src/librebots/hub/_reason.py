import grpc.aio
from google.protobuf.message import DecodeError
from google.rpc import error_details_pb2, status_pb2  # pyright: ignore[reportMissingTypeStubs]

_DOMAIN = "hub.libre-bots"
_DETAILS_KEY = "grpc-status-details-bin"


def reason_of(error: grpc.aio.AioRpcError) -> str | None:
    """Return the hub reason name carried by the error, or None if there is none."""
    for key, value in error.trailing_metadata() or ():
        if key != _DETAILS_KEY or not isinstance(value, bytes):
            continue
        status = status_pb2.Status()
        try:
            status.ParseFromString(value)
        except DecodeError:
            return None
        for detail in status.details:
            info = error_details_pb2.ErrorInfo()
            try:
                if detail.Unpack(info) and info.domain == _DOMAIN:
                    return info.reason
            except DecodeError:
                continue
    return None
