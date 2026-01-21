# Chamael

Chamael includes:

- Go implementation of Kronos

**Note**: plz place the project directory in the **user's home directory**.

if run failed, you need to run pkill:

``` bash
pkill -f "go_file_name"
```

go_file_name = main


## 1. Go implementation of Kronos

Chamael can be run in 3 ways:
- Local
- All nodes in one Docker
- One node in one Docker

### 1.1 Local

Run on your local machine, one node is one process.

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


### All nodes in one Docker

Build a docker including all nodes in it.

Generate node config files based on `cmd/main/config_local.yaml`:
``` bash
go run ./cmd/configMaker/configMaker.go -config_path ./cmd/main/config_local.yaml
```

Build Docker image via Dockerfile:
``` bash
docker build -t chamael:latest .
```

Start Docker:
``` bash
docker run -it --rm chamael:latest 4 3 1
```

### One node in one Docker

Using Docker compose to run a docker each node.

Generate node config files based on `cmd/main/config_compose.yaml`:
``` bash
go run ./cmd/configMaker/configMaker.go -config_path ./cmd/main/config_compose.yaml
```

Start Docker:
``` bash
docker compose up
```
