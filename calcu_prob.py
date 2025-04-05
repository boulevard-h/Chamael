import math
from scipy.stats import hypergeom

def shard_corruption_prob(N_total, F, S, corruption_threshold):
    """
    计算单个分片的腐化概率
    :param N_total: 总节点数
    :param F: 恶意节点比例上限(0 < F < 1/3)
    :param S: 分片数
    :param corruption_threshold: 腐化阈值比例(如2/3)
    :return: 分片腐化概率
    """
    n_shard = N_total // S  # 分片大小
    M = math.floor(F * N_total)  # 总恶意节点数
    
    # 计算腐化节点数下限
    x_min = math.ceil(n_shard * corruption_threshold)
    x_max = min(n_shard, M)
    
    # 超几何分布概率求和
    prob = 0.0
    for x in range(x_min, x_max + 1):
        prob += hypergeom.pmf(x, N_total, M, n_shard)
    return prob

def calcu_system_failure_prob(shard_fail_prob, shard_num):
    system_failure_prob = 1 - (1 - shard_fail_prob) ** shard_num
    system_failure_prob_tailor = shard_num * shard_fail_prob
    return system_failure_prob, system_failure_prob_tailor

# 参数示例
N = 2000    # 总节点数
F = 1/4     # 恶意节点比例上限
f = 2/3     # 片内容错
S = N // 117


print(f"\n总节点数：{N}, 分片数: {S}, 片内容错：{f:.2f}, 分片大小：{N//S}")

# 计算单个分片被完全腐化的概率
p_failure = shard_corruption_prob(N, F, S, f)
print(f"单个分片失效概率：{p_failure:e}")

# 计算系统存在分片被腐化的概率（直接和泰勒近似两种）
system_failure_prob, system_failure_prob_tailor = calcu_system_failure_prob(p_failure, S)
print(f"系统失效概率(非近似): {system_failure_prob:e}, 系统失效概率(泰勒近似): {system_failure_prob_tailor:e}")

   