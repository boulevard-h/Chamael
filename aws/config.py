from collections import Counter
from dataclasses import dataclass

import boto3
import yaml


N_M = 4
N_W = 4
F_M = 1
F_W = 1
SHARD_COUNT = 25
WORK_SHARD_NODES_PER_SERVER = 4
START_PORT = 9233
REGION_NAMES = ["us-east-1", "ap-east-1", "ap-northeast-1", "eu-west-2"]
ONLY_RUNNING_INSTANCES = True


@dataclass(frozen=True)
class InstanceMeta:
    instance_id: str
    public_ip: str
    region: str
    instance_type: str
    cpu_cores: int
    vcpus: int


@dataclass(frozen=True)
class ServerAssignment:
    instance: InstanceMeta
    shard_id: int
    start_pid: int
    node_count: int

    @property
    def end_pid(self):
        return self.start_pid + self.node_count - 1


@dataclass(frozen=True)
class AllocationPlan:
    shard0_servers: list[InstanceMeta]
    work_servers: list[InstanceMeta]
    unused_servers: list[InstanceMeta]
    assignments: list[ServerAssignment]


def chunked(items, size):
    for index in range(0, len(items), size):
        yield items[index : index + size]


def total_nodes(mainchain_nodes, work_shard_nodes, shard_count):
    if shard_count <= 0:
        return 0
    return mainchain_nodes + max(0, shard_count - 1) * work_shard_nodes


def shard_size(shard_id, mainchain_nodes, work_shard_nodes):
    return mainchain_nodes if shard_id == 0 else work_shard_nodes


def shard_start_pid(shard_id, mainchain_nodes, work_shard_nodes):
    if shard_id == 0:
        return 0
    return mainchain_nodes + (shard_id - 1) * work_shard_nodes


def pid_to_shard_and_sid(pid, mainchain_nodes, work_shard_nodes, shard_count):
    total = total_nodes(mainchain_nodes, work_shard_nodes, shard_count)
    if pid < 0 or pid >= total:
        raise ValueError(f"pid {pid} is outside [0, {total})")
    if pid < mainchain_nodes:
        return 0, pid
    offset = pid - mainchain_nodes
    shard_id = 1 + offset // work_shard_nodes
    sid = offset % work_shard_nodes
    return shard_id, sid


def fetch_instance_type_cpu_info(ec2_client, instance_types):
    cpu_info_by_type = {}
    for instance_type_chunk in chunked(sorted(set(instance_types)), 100):
        response = ec2_client.describe_instance_types(InstanceTypes=instance_type_chunk)
        for instance_type_info in response["InstanceTypes"]:
            vcpu_info = instance_type_info.get("VCpuInfo", {})
            cpu_info_by_type[instance_type_info["InstanceType"]] = {
                "cpu_cores": vcpu_info.get("DefaultCores", 0),
                "vcpus": vcpu_info.get("DefaultVCpus", 0),
            }
    return cpu_info_by_type


def collect_instances(region_names):
    instances = []
    for region_name in region_names:
        ec2 = boto3.client("ec2", region_name=region_name)
        paginator = ec2.get_paginator("describe_instances")

        raw_instances = []
        for page in paginator.paginate():
            for reservation in page.get("Reservations", []):
                for instance in reservation.get("Instances", []):
                    public_ip = instance.get("PublicIpAddress")
                    if not public_ip:
                        continue
                    if ONLY_RUNNING_INSTANCES and instance.get("State", {}).get("Name") != "running":
                        continue
                    raw_instances.append(instance)

        if not raw_instances:
            continue

        cpu_info_by_type = fetch_instance_type_cpu_info(
            ec2, [instance["InstanceType"] for instance in raw_instances]
        )
        for instance in raw_instances:
            cpu_info = cpu_info_by_type.get(instance["InstanceType"], {})
            instances.append(
                InstanceMeta(
                    instance_id=instance["InstanceId"],
                    public_ip=instance["PublicIpAddress"],
                    region=region_name,
                    instance_type=instance["InstanceType"],
                    cpu_cores=cpu_info.get("cpu_cores", 0),
                    vcpus=cpu_info.get("vcpus", 0),
                )
            )

    return instances


def build_allocation_plan(
    instances,
    mainchain_nodes,
    work_shard_nodes,
    mainchain_byzantine_nodes,
    work_shard_byzantine_nodes,
    shard_count,
    work_shard_nodes_per_server,
    region_names,
):
    if mainchain_nodes <= 0 or work_shard_nodes <= 0 or shard_count <= 0:
        raise ValueError("mainchain_nodes, work_shard_nodes, and shard_count must all be positive")
    if mainchain_byzantine_nodes < 0 or work_shard_byzantine_nodes < 0:
        raise ValueError("byzantine node counts must be non-negative")
    if 3 * mainchain_byzantine_nodes + 1 > mainchain_nodes:
        raise ValueError("mainchain_nodes must satisfy N_M >= 3*F_M+1")
    if shard_count > 1 and 3 * work_shard_byzantine_nodes + 1 > work_shard_nodes:
        raise ValueError("work_shard_nodes must satisfy N_W >= 3*F_W+1")
    if work_shard_nodes_per_server <= 0:
        raise ValueError("work_shard_nodes_per_server must be positive")
    if shard_count > 1 and work_shard_nodes % work_shard_nodes_per_server != 0:
        raise ValueError(
            "work_shard_nodes must be divisible by work_shard_nodes_per_server "
            "to keep work shards at a fixed number of nodes per machine"
        )

    region_order = {region_name: index for index, region_name in enumerate(region_names)}
    required_shard0_servers = mainchain_nodes
    required_work_servers = ((shard_count - 1) * work_shard_nodes) // work_shard_nodes_per_server
    required_total_servers = required_shard0_servers + required_work_servers

    if len(instances) < required_total_servers:
        raise RuntimeError(
            f"need {required_total_servers} AWS servers, but only found {len(instances)} "
            f"(shard0={required_shard0_servers}, work_shards={required_work_servers})"
        )

    sorted_by_cpu = sorted(
        instances,
        key=lambda instance: (
            -instance.cpu_cores,
            -instance.vcpus,
            region_order.get(instance.region, len(region_names)),
            instance.public_ip,
        ),
    )
    shard0_servers = sorted_by_cpu[:required_shard0_servers]
    shard0_server_ids = {instance.instance_id for instance in shard0_servers}

    remaining_servers = [
        instance for instance in instances if instance.instance_id not in shard0_server_ids
    ]
    remaining_servers = sorted(
        remaining_servers,
        key=lambda instance: (
            region_order.get(instance.region, len(region_names)),
            -instance.cpu_cores,
            -instance.vcpus,
            instance.public_ip,
        ),
    )

    work_servers = remaining_servers[:required_work_servers]
    unused_servers = remaining_servers[required_work_servers:]

    assignments = []
    for pid, instance in enumerate(shard0_servers):
        assignments.append(
            ServerAssignment(
                instance=instance,
                shard_id=0,
                start_pid=pid,
                node_count=1,
            )
        )

    next_pid = mainchain_nodes
    work_server_index = 0
    for shard_id in range(1, shard_count):
        remaining_nodes_in_shard = work_shard_nodes
        while remaining_nodes_in_shard > 0:
            node_count = min(work_shard_nodes_per_server, remaining_nodes_in_shard)
            assignments.append(
                ServerAssignment(
                    instance=work_servers[work_server_index],
                    shard_id=shard_id,
                    start_pid=next_pid,
                    node_count=node_count,
                )
            )
            next_pid += node_count
            remaining_nodes_in_shard -= node_count
            work_server_index += 1

    return AllocationPlan(
        shard0_servers=shard0_servers,
        work_servers=work_servers,
        unused_servers=unused_servers,
        assignments=assignments,
    )


def generate_yaml_config(
    assignments,
    mainchain_nodes,
    work_shard_nodes,
    mainchain_byzantine_nodes,
    work_shard_byzantine_nodes,
    shard_count,
    start_port,
):
    ip_list = []
    port_list = []
    for assignment in assignments:
        for pid in range(assignment.start_pid, assignment.end_pid + 1):
            _, sid = pid_to_shard_and_sid(pid, mainchain_nodes, work_shard_nodes, shard_count)
            ip_list.append(assignment.instance.public_ip)
            port_list.append(str(start_port + sid))

    config = {
        "N_M": mainchain_nodes,
        "N_W": work_shard_nodes,
        "F_M": mainchain_byzantine_nodes,
        "F_W": work_shard_byzantine_nodes,
        "m": shard_count,
        "IPList": ip_list,
        "PID": 0,
        "SID": 0,
        "Snum": 0,
        "PortList": port_list,
        "Prepare": 10,
        "Statistic": "./statistics",
        "WaitEpoch": 12,
        "WaitBuf": 40,
        "MessageBuffer": 4096,
        "TrackTraffic": True,
        "Txnum": 1000,
        "Crate": 0.1,
        "TestEpochs": 3,
    }

    return yaml.dump(config, sort_keys=False, default_flow_style=False)


def render_bash_array(name, values):
    lines = [f"{name}=("]
    for index, value in enumerate(values):
        if isinstance(value, str):
            lines.append(f"[{index}]='{value}'")
        else:
            lines.append(f"[{index}]={value}")
    lines.append(")")
    return lines


def generate_bash_script(assignments):
    script_lines = [
        "#!/bin/bash",
        "",
        "# the number of AWS servers to remove",
        f"N={len(assignments)}",
        "",
        "# the number of nodes hosted on each AWS server",
    ]
    script_lines.extend(render_bash_array("nodeCountsVar", [assignment.node_count for assignment in assignments]))
    script_lines.extend(
        [
            "",
            "# public IPs --- This is the public IPs of AWS servers",
        ]
    )
    script_lines.extend(render_bash_array("pubIPsVar", [assignment.instance.public_ip for assignment in assignments]))
    script_lines.append("")
    return "\n".join(script_lines)


def format_pid_range(start_pid, end_pid):
    if start_pid == end_pid:
        return str(start_pid)
    return f"{start_pid}-{end_pid}"


def format_shard_region_summary(assignments, shard_id, region_names):
    region_counter = Counter(
        assignment.instance.region for assignment in assignments if assignment.shard_id == shard_id
    )
    ordered_regions = []
    for region_name in region_names:
        if region_name in region_counter:
            ordered_regions.append(f"{region_name} x{region_counter[region_name]}")
    for region_name in sorted(region_counter.keys()):
        if region_name not in region_names:
            ordered_regions.append(f"{region_name} x{region_counter[region_name]}")
    if not ordered_regions:
        return "none"
    return ", ".join(ordered_regions)


def print_allocation_summary(
    instances,
    plan,
    mainchain_nodes,
    work_shard_nodes,
    mainchain_byzantine_nodes,
    work_shard_byzantine_nodes,
    shard_count,
    region_names,
):
    print("Allocation summary:")
    print(f"- discovered AWS servers: {len(instances)}")
    print(
        f"- required AWS servers: {len(plan.assignments)} "
        f"(shard0={len(plan.shard0_servers)}, work_shards={len(plan.work_servers)})"
    )
    print(
        f"- shard sizes/faults: shard0={mainchain_nodes}/{mainchain_byzantine_nodes}, "
        f"worker={work_shard_nodes}/{work_shard_byzantine_nodes}"
    )
    print("- shard0 selection order: cpu_cores desc, then vcpus desc")
    print(f"- unused AWS servers: {len(plan.unused_servers)}")

    print("\nShard region summary:")
    for shard_id in range(shard_count):
        print(f"- shard{shard_id}: {format_shard_region_summary(plan.assignments, shard_id, region_names)}")

    print("\nServer assignments:")
    for index, assignment in enumerate(plan.assignments):
        role = "mainchain" if assignment.shard_id == 0 else f"shard{assignment.shard_id}"
        _, shard_sid_start = pid_to_shard_and_sid(
            assignment.start_pid, mainchain_nodes, work_shard_nodes, shard_count
        )
        _, shard_sid_end = pid_to_shard_and_sid(
            assignment.end_pid, mainchain_nodes, work_shard_nodes, shard_count
        )
        print(
            f"[{index:03d}] {role:<9} "
            f"pid={format_pid_range(assignment.start_pid, assignment.end_pid):<9} "
            f"sid={format_pid_range(shard_sid_start, shard_sid_end):<7} "
            f"nodes={assignment.node_count:<2} "
            f"region={assignment.instance.region:<16} "
            f"type={assignment.instance.instance_type:<18} "
            f"cores={assignment.instance.cpu_cores:<3} "
            f"vcpus={assignment.instance.vcpus:<3} "
            f"ip={assignment.instance.public_ip}"
        )

    if plan.unused_servers:
        print("\nUnused servers:")
        for instance in plan.unused_servers:
            print(
                f"- region={instance.region} "
                f"type={instance.instance_type} "
                f"cores={instance.cpu_cores} "
                f"vcpus={instance.vcpus} "
                f"ip={instance.public_ip}"
            )


def main():
    instances = collect_instances(REGION_NAMES)
    plan = build_allocation_plan(
        instances=instances,
        mainchain_nodes=N_M,
        work_shard_nodes=N_W,
        mainchain_byzantine_nodes=F_M,
        work_shard_byzantine_nodes=F_W,
        shard_count=SHARD_COUNT,
        work_shard_nodes_per_server=WORK_SHARD_NODES_PER_SERVER,
        region_names=REGION_NAMES,
    )

    yaml_config = generate_yaml_config(
        assignments=plan.assignments,
        mainchain_nodes=N_M,
        work_shard_nodes=N_W,
        mainchain_byzantine_nodes=F_M,
        work_shard_byzantine_nodes=F_W,
        shard_count=SHARD_COUNT,
        start_port=START_PORT,
    )
    bash_script = generate_bash_script(plan.assignments)

    print_allocation_summary(instances, plan, N_M, N_W, F_M, F_W, SHARD_COUNT, REGION_NAMES)
    print("\n\nYAML config:")
    print(yaml_config)
    print("\n\nBash header:")
    print(bash_script)


if __name__ == "__main__":
    main()
