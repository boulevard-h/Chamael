#!/bin/bash

# Check if parameters are provided
if [ -z "$1" ] || [ -z "$2" ] || [ -z "$3" ] || [ -z "$4" ]; then
  echo "Usage: $0 <min_PID> <max_PID> <mode> <start_time>"
  exit 1
fi

min_PID="$1"  
max_PID="$2"
mode="$3"
start_time="$4"
for (( i=min_PID; i<=max_PID; i++ ))
do
  ./start_one.sh $i $mode 0 "$start_time" &
done

wait

# go run ./cmd/performance/performanceCal.go