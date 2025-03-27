#!/bin/bash

# Check if parameters are provided
if [ -z "$1" ] || [ -z "$2" ] || [ -z "$3" ]; then
  echo "Usage: $0 <min_PID> <max_PID> <Debug(0 or 1)>"
  exit 1
fi

min_PID="$1"
max_PID="$2"
DEBUG="$3"
config_dir="$HOME/Chamael/configs"

for (( i=min_PID; i<=max_PID; i++ ))
do
  config_file="$config_dir/config_$i.yaml"
  echo "Using config file: $config_file"
  go run ./cmd/reConfig/RCnode.go $config_file $DEBUG &
done