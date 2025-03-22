#!/bin/bash

# Check if parameters are provided
if [ -z "$1" ] || [ -z "$2" ]; then
  echo "Usage: $0 <N> <Debug(0 or 1)>"
  exit 1
fi

N="$1"
DEBUG="$2"
config_dir="$HOME/Chamael/configs"

for (( i=0; i<N; i++ ))
do
  config_file="$config_dir/config_$i.yaml"
  echo "Using config file: $config_file"
  go run ./cmd/reConfig/RCnode.go $config_file $DEBUG &
done