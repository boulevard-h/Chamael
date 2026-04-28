# Areopagus

Go implementation of Areopagus

**Note**: place the project directory at `$HOME/Areopagus`.

If a previous local run is still active, stop existing processes before restarting:

``` bash
pkill -f main
```

Areopagus runs locally (one node = one process).

Install dependencies:
``` bash
go mod download
```

Generate node config files based on `cmd/main/config_local.yaml`:
``` bash
go run ./cmd/configMaker/configMaker.go -config_path ./cmd/main/config_local.yaml
```

Start all nodes via shell script:
``` bash
./start_all.sh min_PID max_PID mode start_time
```

To get performance metrics:
``` bash
go run cmd/performance/performanceCal.go
```
