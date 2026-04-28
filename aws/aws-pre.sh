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

# 上传配置文件到所有AWS服务器
offset=0
i=0
while [ $i -le $(( N-1 )) ]; do
    node_count=${nodeCountsVar[i]}
    start_node=$offset
    end_node=$(( offset + node_count - 1 ))
    (
    echo "[➤] 开始上传到服务器 ${pubIPsVar[i]}"

    # 上传该服务器需要的配置文件
    for (( j=start_node; j<=end_node; j++ )); do
        scp -q -o "StrictHostKeyChecking no" -i "/home/ubuntu/Areopagus.pem" \
            "/home/ubuntu/Areopagus/configs/config_${j}.yaml" \
            ubuntu@${pubIPsVar[i]}:/home/ubuntu/Areopagus/configs/
    done
    
    echo "[✓] 完成服务器 ${pubIPsVar[i]}"
    ) &
    offset=$(( offset + node_count ))
    i=$(( i+1 ))
done

wait
echo "所有文件同步完成"
