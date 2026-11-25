import re
import tomllib
from pathlib import Path

from librebots.hub.v1 import stats_pb2_grpc

SRC = Path(__file__).resolve().parents[1] / "src"


def test_grpcio_lower_bound_matches_the_generated_code() -> None:
    pyproject = tomllib.loads((Path(__file__).parents[1] / "pyproject.toml").read_text())
    bounds = [m.group(1) for d in pyproject["project"]["dependencies"] if (m := re.fullmatch(r"grpcio>=(\S+)", d))]
    assert bounds == [stats_pb2_grpc.GRPC_GENERATED_VERSION]


def test_namespace_has_no_init() -> None:
    assert not (SRC / "librebots" / "__init__.py").exists()


def test_hub_package_is_typed() -> None:
    assert (SRC / "librebots" / "hub" / "py.typed").is_file()
