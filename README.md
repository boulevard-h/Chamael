# Kronos-NG


**Note**: plz place the project directory in the **user's home directory**.

if run failed, you need to run pkill:

``` bash
pkill -f "go_file_name"
```

## Run

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
