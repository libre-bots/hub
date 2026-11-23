import re
import tomllib
from pathlib import Path

from libre.hub.v1 import stats_pb2, stats_pb2_grpc


def test_grpcio_lower_bound_matches_the_generated_code() -> None:
    pyproject = tomllib.loads((Path(__file__).parents[1] / "pyproject.toml").read_text())
    bounds = [m.group(1) for d in pyproject["project"]["dependencies"] if (m := re.fullmatch(r"grpcio>=(\S+)", d))]
    assert bounds == [stats_pb2_grpc.GRPC_GENERATED_VERSION]


def test_generated_modules_are_marked_as_typed() -> None:
    assert Path(stats_pb2.__file__).with_name("py.typed").is_file()
