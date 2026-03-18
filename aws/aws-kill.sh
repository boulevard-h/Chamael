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

# kill 所有远程机器上名为 main 的进程
i=0
while [ $i -le $(( N-1 )) ]; do
    ssh -o "StrictHostKeyChecking no" -i "/home/ubuntu/Chamael.pem" ubuntu@${pubIPsVar[i]} \
    "pkill -f './main' 2>/dev/null; echo 'killed main on ${pubIPsVar[i]}'" &
    i=$(( i+1 ))
done

wait
echo "所有服务器上的 main 进程已终止"
