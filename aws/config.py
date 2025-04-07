import boto3
import yaml


def extract_unique_public_ips(nested_dict, key_to_extract):
    # 保持原函数不变
    def recurse_items(current_item):
        if isinstance(current_item, dict):
            for key, value in current_item.items():
                if key == key_to_extract:
                    unique_ips.add(value)
                else:
                    recurse_items(value)
        elif isinstance(current_item, list):
            for item in current_item:
                recurse_items(item)

    unique_ips = set()
    recurse_items(nested_dict)
    return list(unique_ips)


def generate_yaml_config(public_ips, nodes_per_server, start_port):
    # server_number = len(public_ips) 使用的服务器数量
    # nodes_per_server = 4 每台服务器上部署的节点数
    N = 72 #每个分片中的节点数量
    F = 24 #每个分片中的恶意节点数量
    m = 7 #分片个数

    # 生成IP列表（每个IP重复nodes_per_server次）
    IPList = []
    for ip in public_ips:
        IPList.extend([ip] * nodes_per_server)

    # 生成端口列表
    PortList = [start_port + (i % N) for i in range(len(IPList))]

    # 构建配置字典
    config = {
        'N': N,
        'F': F,
        'm': m,
        'IPList': IPList,
        'PID': 0,
        'SID': 1,
        'Snum': 2,
        'PortList': PortList,
        'PrepareTime': 100,
        'Statistic': './statistics',
        'WaitTime': 120,
        'Txnum': 1000,
        'Crate': 0.1,
        'TestEpochs': 5
    }

    return yaml.dump(config, sort_keys=False, default_flow_style=False)


def generate_bash_script(public_ips, node):
    """
    生成一个Bash脚本，该脚本使用给定的公共IP地址。

    :param public_ips: 公共IP地址的列表
    :param node: 每个服务器上的节点数，默认为4
    :return: Bash脚本的字符串表示
    """
    n = len(public_ips)  # AWS服务器的数量

    script_lines = [
        "#!/bin/bash",
        "",
        f"# the number of AWS servers to remove",
        f"N={n}",
        "",
        f"# the number of nodes on each server",
        f"node={node}",
        "",
        f"# public IPs --- This is the public IPs of AWS servers"
    ]

    # 添加公共IP地址
    pub_ips_lines = ["pubIPsVar=("]
    i = 0
    for ip in public_ips:
        pub_ips_lines.append(f"[{i}]='{ip}'")
        i += 1
    pub_ips_lines.append(")")

    # 合并脚本行
    script_lines += pub_ips_lines
    script_lines.append("")
    return "\n".join(script_lines)


# 获取各区域的公共IP
region_names = ['us-east-1', 'ap-east-1', 'ap-northeast-1', 'eu-west-2']
region_to_ips = {}

# 按区域获取IP并存储
for region_name in region_names:
    ec2 = boto3.client('ec2', region_name=region_name)
    response = ec2.describe_instances()
    region_ips = extract_unique_public_ips(response, 'PublicIpAddress')
    if region_ips:  # 只有当找到IP时才添加
        region_to_ips[region_name] = region_ips

# 按区域组织的IP列表
sorted_unique_public_ips = []
for region_name in region_names:
    if region_name in region_to_ips:
        sorted_unique_public_ips.extend(region_to_ips[region_name])

# 生成YAML配置
yaml_config = generate_yaml_config(
    public_ips=sorted_unique_public_ips,
    nodes_per_server=4,
    start_port=9233
)

# 生成bash脚本
bash_script = generate_bash_script(sorted_unique_public_ips, 4)

# 输出结果
print("YAML配置：")
print(yaml_config)
print("\n\n\n\nBash脚本：")
print(bash_script)