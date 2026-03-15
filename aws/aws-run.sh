#!/bin/bash

# the number of AWS servers to remove
N=xxx

# the number of nodes on each server
node=xxx

# public IPs --- This is the public IPs of AWS servers
pubIPsVar=(
[0]='xxx'
[1]='xxx'
[2]='xxx'
)

# 命令执行
i=0
while [ $i -le $(( N-1 )) ]; do
    ssh -o "StrictHostKeyChecking no" -i "/home/ubuntu/Chamael.pem" ubuntu@${pubIPsVar[i]} \
    "cd Chamael && rm -rf /home/ubuntu/Chamael/log/* 2>/dev/null; 
     nohup ./start_all.sh $(( i * node )) $(( (i+1) * node-1 )) 0 \"2025-03-30 03:08:00.000\" > server-$i.out" &
    i=$(( i+1 ))
done