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

# 远程更新代码并预热 Go build cache
i=0
while [ $i -le $(( N-1 )) ]; do
    (
    echo "[➤] 开始处理服务器 ${pubIPsVar[i]}"

    ssh -o "StrictHostKeyChecking no" -i "/home/ubuntu/Areopagus.pem" ubuntu@${pubIPsVar[i]} '
        set -e
        cd /home/ubuntu/Areopagus

        echo "[1/3] git pull"
        git pull --ff-only

        echo "[2/3] warm up cmd/main/main.go build cache"
        go run ./cmd/main/main.go /tmp/areopagus-cache-warmup-missing.yaml 0 "2099-01-01 00:00:00.000" >/tmp/areopagus-main-warmup.log 2>&1 || true

        echo "[3/3] warm up cmd/txsMaker/txsMaker.go build cache"
        go run ./cmd/txsMaker/txsMaker.go >/tmp/areopagus-txsmaker-warmup.log 2>&1 || true
    '

    status=$?
    if [ $status -eq 0 ]; then
        echo "[✓] 完成服务器 ${pubIPsVar[i]}"
    else
        echo "[✗] 失败服务器 ${pubIPsVar[i]} (exit=${status})"
    fi
    ) &
    i=$(( i+1 ))
done

wait
echo "所有服务器处理完成"
