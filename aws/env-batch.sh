#!/bin/bash

sudo add-apt-repository -y ppa:longsleep/golang-backports
sudo apt update
sudo apt install -y golang

cd /home/ubuntu
git clone https://github.com/hidden-er/Chamael.git
cd Chamael
#go env -w GOPROXY=https://goproxy.cn,direct
go mod tidy