from collections.abc import AsyncIterator, Callable
from typing import Any, Final

import grpc
import grpc.aio

KEEPALIVE_OPTIONS: Final[tuple[tuple[str, int], ...]] = (
    ("grpc.keepalive_time_ms", 30000),
    ("grpc.keepalive_timeout_ms", 10000),
    ("grpc.keepalive_permit_without_calls", 1),
    ("grpc.http2.max_pings_without_data", 0),
)


def _with_token(details: grpc.aio.ClientCallDetails, token: str) -> grpc.aio.ClientCallDetails:
    metadata = grpc.aio.Metadata(*(details.metadata or ()))
    metadata.add("authorization", f"Bearer {token}")
    return grpc.aio.ClientCallDetails(
        details.method, details.timeout, metadata, details.credentials, details.wait_for_ready
    )


class _UnaryTokenInterceptor(grpc.aio.UnaryUnaryClientInterceptor):
    def __init__(self, token: str) -> None:
        self._token = token

    async def intercept_unary_unary(
        self,
        continuation: Callable[[grpc.aio.ClientCallDetails, Any], Any],
        client_call_details: grpc.aio.ClientCallDetails,
        request: Any,
    ) -> Any:
        return await continuation(_with_token(client_call_details, self._token), request)


class _StreamTokenInterceptor(grpc.aio.UnaryStreamClientInterceptor):
    def __init__(self, token: str) -> None:
        self._token = token

    async def intercept_unary_stream(
        self,
        continuation: Callable[[grpc.aio.ClientCallDetails, Any], Any],
        client_call_details: grpc.aio.ClientCallDetails,
        request: Any,
    ) -> AsyncIterator[Any]:
        return await continuation(_with_token(client_call_details, self._token), request)


def connect(address: str, token: str, *, tls: bool, root_certificates: bytes | None = None) -> grpc.aio.Channel:
    """Return a channel that sends the bearer token on every call; tls=False sends it unencrypted."""
    interceptors: list[grpc.aio.ClientInterceptor] = [_UnaryTokenInterceptor(token), _StreamTokenInterceptor(token)]
    if tls:
        credentials = grpc.ssl_channel_credentials(root_certificates=root_certificates)
        return grpc.aio.secure_channel(address, credentials, options=KEEPALIVE_OPTIONS, interceptors=interceptors)
    return grpc.aio.insecure_channel(address, options=KEEPALIVE_OPTIONS, interceptors=interceptors)
