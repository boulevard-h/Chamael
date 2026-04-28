import math
from scipy.stats import hypergeom

def shard_corruption_prob(N_total, F, S, corruption_threshold):
    """
    Compute the corruption probability for one shard.
    :param N_total: total number of nodes
    :param F: upper bound on the Byzantine-node ratio (0 < F < 1/3)
    :param S: number of shards
    :param corruption_threshold: corruption threshold ratio, such as 2/3
    :return: shard corruption probability
    """
    n_shard = N_total // S  # shard size
    M = math.floor(F * N_total)  # total number of Byzantine nodes

    # Lower bound on the number of corrupted nodes.
    x_min = math.ceil(n_shard * corruption_threshold)
    x_max = min(n_shard, M)

    # Sum hypergeometric probabilities.
    prob = 0.0
    for x in range(x_min, x_max + 1):
        prob += hypergeom.pmf(x, N_total, M, n_shard)
    return prob

def calcu_system_failure_prob(shard_fail_prob, shard_num):
    system_failure_prob = 1 - (1 - shard_fail_prob) ** shard_num
    system_failure_prob_tailor = shard_num * shard_fail_prob
    return system_failure_prob, system_failure_prob_tailor

# Example parameters.
N = 2000    # total number of nodes
F = 1/4     # upper bound on the Byzantine-node ratio
f = 2/3     # intra-shard fault tolerance
S = N // 117


print(f"\nTotal nodes: {N}, shards: {S}, intra-shard fault tolerance: {f:.2f}, shard size: {N//S}")

# Compute the probability that one shard is corrupted.
p_failure = shard_corruption_prob(N, F, S, f)
print(f"Single-shard failure probability: {p_failure:e}")

# Compute the probability that at least one shard is corrupted, both exactly and with Taylor approximation.
system_failure_prob, system_failure_prob_tailor = calcu_system_failure_prob(p_failure, S)
print(f"System failure probability (exact): {system_failure_prob:e}, system failure probability (Taylor approximation): {system_failure_prob_tailor:e}")


