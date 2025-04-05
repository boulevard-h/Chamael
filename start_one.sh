#!/bin/bash

# Check if parameters are provided
if [ -z "$1" ] || [ -z "$2" ] || [ -z "$3" ] || [ -z "$4" ]; then
  echo "Usage: $0 <id> <mode> <print_perf> <start_time>"
  exit 1
fi

id="$1"
mode="$2"
print_perf="$3"
start_time="$4"
# Directory containing the config files
config_dir="$HOME/Chamael/configs"
config_file="$config_dir/config_$id.yaml"

# Check if the config file exists
if [ -f "$config_file" ]; then
    echo "Using config file: $config_file"
    
    # Read Txnum and Crate parameters from config file and calculate tx_num
    Txnum=$(grep '^Txnum:' "$config_file" | awk '{print $2}')
    Crate=$(grep '^Crate:' "$config_file" | awk '{print $2}')
    TestEpochs=$(grep '^TestEpochs:' "$config_file" | awk '{print $2}')
    tx_num=$(echo "$Txnum * $Crate * $TestEpochs" | bc -l)
    tx_num=$(printf "%.0f" "$tx_num")
    
    # 从 config 读取参数 N
    N=$(grep '^"N":' "$config_file" | awk '{print $2}')
    # 将 tx_num 除以 N
    tx_num=$(echo "$tx_num / $N" | bc -l)
    tx_num=$(printf "%.0f" "$tx_num")
    
    # 从 config 读取参数 m
    m=$(grep '^m:' "$config_file" | awk '{print $2}')
    echo "Cross-shard tx_num: $tx_num"

    # Call txsMaker program with tx_num parameter
    go run ./cmd/txsMaker --id $id --shard_num $m --tx_num $tx_num --Rrate 10

    # Call main program with config file and mode
    go run ./cmd/main "$config_file" "$mode" "$start_time"
else
    echo "Config file $config_file not found"
fi

if [ "$print_perf" -eq 1 ]; then
    cat "./log/(Performance)node$id"
fi
