#!/bin/bash

# Check if parameters are provided
if [ -z "$1" ] || [ -z "$2" ] || [ -z "$3" ]; then
  echo "Usage: $0 <N> <m> <mode>"
  exit 1
fi

N="$1"  
m="$2"
mode="$3"

for (( i=0; i<N*m; i++ ))
do
  ./start_one.sh $i $mode 0 &
done

wait

go run ./cmd/performance/performanceCal.go