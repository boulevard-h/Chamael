# AWS分布式部署

### 节点服务器环境配置

```shell
#推荐使用Ubuntu22.04 LTS
/home/ubuntu目录上传env-batch.sh
sudo apt update
sudo apt install dos2unix
dos2unix env-batch.sh
chmod 777 env-batch.sh
./env-batch.sh
#测试Chamael四个测试的运行情况（go编译）
#关机存映像，用该映像重启服务器，存模版
```



## 运行流程

### 环境配置

**本地windows**：装有`aws-cli`，完成`aws configure`配置，python有`boto3`；

**远程Chamael中控**：环境配置同节点服务器，额外在根目录放置私钥（权限400）



### 具体操作

#### （1）启动各个区域的AWS服务器

推荐在各个区域创建好支持Chamael运行环境的服务器模版，直接从模版创建实例。

#### （2）部署配置文件

* 在`config.py`中设置参数：

| 函数名称::参数名称                      | 参数含义                 |
| --------------------------------------- | ------------------------ |
| generate_yaml_config:::nodes_per_server | 每台服务器上部署的节点数 |
| generate_yaml_config:::N                | 每个分片中的节点数量     |
| generate_yaml_config:::M                | 分片个数                 |
| generate_bash_script:::node             | 每台服务器上部署的节点数 |

* 本地运行`config.py`，将生成的YAML配置拷贝替换到`config_local.yaml`(整体)；将生成的Bash脚本拷贝替换到`aws-pre.txt`、`aws-run.txt`和`aws-log.txt`的对应位置。

* 将`aws-pre.txt`、`aws-run.txt`和`aws-log.txt`上传到**Chamael中控的/home/ubuntu目录下**；将`config_local.yaml`上传到**Chamael中控的/home/ubuntu/Chamael/cmd/main目录下**。

* 在**Chamael中控的/home/ubuntu/Chamael目录下**运行

  ```shell
  #刚需
  go run ./cmd/configMaker/configMaker.go -config_path ./cmd/main/config_local.yaml
  #用于NS测试 (<NSShard>发生分叉的分片编号)
  go run cmd/eviMaker/eviMaker.go <N> <F> <M> <NSShard>
  ```

* 在**Chamael中控的/home/ubuntu目录下**运行	

  ```shell
  dos2unix aws-log.txt aws-pre.txt aws-run.txt
  ./aws-pre.txt
  ```

​	向各个节点服务器的**Chamael/configs/\* **和 **Chamael/cmd/noSafety/NS.yaml** 传入**一致的**配置文件。

​	`NS.yaml`与`configs/*`内容相关联，每次都需重新生成；`NL.yaml`和`RC.yaml`只需各节点一致即可，可以一直沿用模版中的。

#### （3）运行与获取日志数据

##### kronos

* 编辑`aws-run.txt`：

  ```shell
   ./start_all.sh $(( i * node )) $(( (i+1) * node-1 )) 0 \"2025-03-30 03:08:00.000\"
  ```

​	只需要调整这句命令里的0/1(分别对应有无debug日志)和起始运行时间即可。

* 在**Chamael中控的/home/ubuntu目录下**运行`./aws-run.txt`，完成之后运行`./aws-log.txt`

* 在**Chamael中控的/home/ubuntu/Chamael目录下**运行

  ```shell
  go run ./cmd/performance/performanceCal.go
  ```

​	获取正常执行流程中的TPS和时延数据。

##### NL

* 编辑`aws-run.txt`：

  ```shell
   ./start_NLTest.sh $(( i * node )) $(( (i+1) * node-1 )) 0 \"2025-03-30 03:08:00.000\"
  ```

​	只需要调整这句命令里的0/1(分别对应有无debug日志)和起始运行时间即可。

* 在**Chamael中控的/home/ubuntu目录下**运行`./aws-run.txt`，完成之后运行`./aws-log.txt`

* 在**Chamael中控的/home/ubuntu/Chamael目录下**运行

  ```shell
  go run ./cmd/duration/durationCal.go
  ```

##### NS

* 编辑`aws-run.txt`：

  ```shell
   ./start_NSTest.sh $(( i * node )) $(( (i+1) * node-1 )) 0 \"2025-03-30 03:08:00.000\"
  ```

​	只需要调整这句命令里的0/1(分别对应有无debug日志)和起始运行时间即可。

* 在**Chamael中控的/home/ubuntu目录下**运行`./aws-run.txt`，完成之后运行`./aws-log.txt`

* 在**Chamael中控的/home/ubuntu/Chamael目录下**运行

  ```shell
  go run ./cmd/duration/durationCal.go
  ```

##### NL

* 编辑`aws-run.txt`：

  ```shell
   ./start_ReConfig.sh $(( i * node )) $(( (i+1) * node-1 )) 0 \"2025-03-30 03:08:00.000\"
  ```

​	只需要调整这句命令里的0/1(分别对应有无debug日志)和起始运行时间即可。

* 在**Chamael中控的/home/ubuntu目录下**运行`./aws-run.txt`，完成之后运行`./aws-log.txt`

* 在**Chamael中控的/home/ubuntu/Chamael目录下**运行

  ```shell
  go run ./cmd/duration/durationCal.go
  ```

