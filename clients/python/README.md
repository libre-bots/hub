# librebots-hub

The Python client for the Libre Bots hub.

```sh
pip install librebots-hub
```

```python
import asyncio

from librebots.hub import connect
from librebots.hub.v1 import stats_pb2, stats_pb2_grpc


async def main() -> None:
    async with connect("localhost:50051", "dev-token", tls=False) as channel:
        stats = await stats_pb2_grpc.StatsServiceStub(channel).GetStats(stats_pb2.GetStatsRequest())
        print(stats.total.guild_count)


asyncio.run(main())
```
