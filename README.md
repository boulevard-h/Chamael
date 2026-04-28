# Areopagus

Go implementation of Areopagus.

Areopagus is a sharded Byzantine fault-tolerant protocol implementation for
local artifact evaluation. A local run starts one process per node and writes
runtime logs under `$HOME/Areopagus/log/`.

**Note**: place the project directory at `$HOME/Areopagus`.

## Repository Layout

- `cmd/main`: node entry point and the local experiment template.
- `cmd/configMaker`: generates per-node configuration files.
- `cmd/txsMaker`: generates local transaction input data.
- `cmd/performance`: aggregates throughput and timing metrics from logs.
- `internal/bft`: Areopagus protocol logic, including worker-shard RBC,
  cross-shard transaction distribution, main-shard bitmap handling, and metrics.
- `internal/mvba`: MVBA and SPB implementation.
- `internal/party`: node runtime state, networking channels, timing, and traffic accounting.
- `pkg/core`: message encapsulation, dispatch, send, and receive paths.
- `pkg/config`: configuration loading and shard layout helpers.
- `pkg/protobuf`: protocol message definitions and generated Go bindings.
- `pkg/crypto`: Crypto components used by the protocol.
- `pkg/txs` and `pkg/utils`: transaction generation, local storage, logging, and shared utilities.
- `configs`: generated per-node configuration files.

## Requirements

- Go 1.18 or newer.
- A Unix-like shell environment for the provided scripts.

Install dependencies:

``` bash
go mod download
```

## Local Run

Generate node config files based on `cmd/main/config_local.yaml`:

``` bash
go run ./cmd/configMaker/configMaker.go -config_path ./cmd/main/config_local.yaml
```

Start all nodes via shell script:

``` bash
./start_all.sh min_PID max_PID mode start_time
```

Collect performance metrics:

``` bash
go run cmd/performance/performanceCal.go
```
