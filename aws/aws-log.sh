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

# 递归收集日志（保留目录结构）
i=0
while [ $i -le $(( N-1 )) ]; do
    scp -r -o "StrictHostKeyChecking no" -i "/home/ubuntu/Chamael.pem" \
        ubuntu@${pubIPsVar[i]}:"/home/ubuntu/Chamael/log/*" \
        /home/ubuntu/Chamael/log/ &
    i=$((i+1))
done

wait
echo "所有日志已合并到: /home/ubuntu/Chamael/log"