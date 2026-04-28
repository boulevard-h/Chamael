#!/bin/bash

# the number of AWS servers to remove
N=xxx

# the number of nodes hosted on each AWS server
nodeCountsVar=(
[0]=xxx
[1]=xxx
[2]=xxx
)

# public IPs --- This is the public IPs of AWS servers
pubIPsVar=(
[0]='xxx'
[1]='xxx'
[2]='xxx'
)

# 命令执行
offset=0
i=0
while [ $i -le $(( N-1 )) ]; do
    node_count=${nodeCountsVar[i]}
    start_node=$offset
    end_node=$(( offset + node_count - 1 ))
    ssh -o "StrictHostKeyChecking no" -i "/home/ubuntu/Areopagus.pem" ubuntu@${pubIPsVar[i]} \
    "cd Areopagus && rm -rf /home/ubuntu/Areopagus/log/* 2>/dev/null; 
     nohup ./start_all.sh ${start_node} ${end_node} 0 \"2025-03-30 03:08:00.000\" > server-$i.out" &
    offset=$(( offset + node_count ))
    i=$(( i+1 ))
done
