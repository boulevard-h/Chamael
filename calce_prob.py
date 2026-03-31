import argparse
import math
from fractions import Fraction


def log_comb(n: int, k: int) -> float:
    if k < 0 or k > n:
        return float("-inf")
    return math.lgamma(n + 1) - math.lgamma(k + 1) - math.lgamma(n - k + 1)


def hypergeom_tail_prob(total_nodes: int, malicious_nodes: int, sample_size: int, bad_min: int) -> float:
    """
    Return P[X >= bad_min] where:
      X ~ Hypergeom(total_nodes, malicious_nodes, sample_size)
    """
    bad_max = min(sample_size, malicious_nodes)
    if bad_min > bad_max:
        return 0.0

    log_denom = log_comb(total_nodes, sample_size)
    log_terms = []
    for bad_nodes in range(bad_min, bad_max + 1):
        log_pmf = (
            log_comb(malicious_nodes, bad_nodes)
            + log_comb(total_nodes - malicious_nodes, sample_size - bad_nodes)
            - log_denom
        )
        log_terms.append(log_pmf)

    peak = max(log_terms)
    return math.exp(peak) * sum(math.exp(log_term - peak) for log_term in log_terms)


def floor_fraction_product(count: int, ratio: Fraction) -> int:
    return (count * ratio.numerator) // ratio.denominator


def parse_ratio(value: str) -> Fraction:
    try:
        ratio = Fraction(value)
    except (ValueError, ZeroDivisionError) as exc:
        raise argparse.ArgumentTypeError(f"Invalid rational value: {value}") from exc
    return ratio


def mainchain_failure_prob(
    total_nodes: int,
    malicious_ratio: Fraction,
    mainchain_size: int,
    malicious_threshold: Fraction,
) -> float:
    malicious_nodes = floor_fraction_product(total_nodes, malicious_ratio)
    bad_min = floor_fraction_product(mainchain_size, malicious_threshold) + 1
    return hypergeom_tail_prob(total_nodes, malicious_nodes, mainchain_size, bad_min)


def find_min_mainchain_size(
    total_nodes: int,
    malicious_ratio: Fraction,
    malicious_threshold: Fraction,
    epsilon: float,
):
    malicious_nodes = floor_fraction_product(total_nodes, malicious_ratio)

    for mainchain_size in range(1, total_nodes + 1):
        failure_prob = mainchain_failure_prob(
            total_nodes=total_nodes,
            malicious_ratio=malicious_ratio,
            mainchain_size=mainchain_size,
            malicious_threshold=malicious_threshold,
        )
        if failure_prob <= epsilon:
            return mainchain_size, mainchain_size / total_nodes, failure_prob, malicious_nodes

    return None, None, None, malicious_nodes


def parse_args():
    parser = argparse.ArgumentParser(
        description=(
            "Search the minimum main-chain size m such that "
            "P(malicious ratio on main chain > threshold) <= epsilon."
        )
    )
    parser.add_argument("--total-nodes", type=int, default=2000, help="Total number of nodes.")
    parser.add_argument(
        "--malicious-ratio",
        type=parse_ratio,
        default=Fraction(1, 4),
        help="Global malicious node ratio. Supports forms like 1/4 or 0.25.",
    )
    parser.add_argument(
        "--threshold",
        type=parse_ratio,
        default=Fraction(1, 3),
        help="Maximum acceptable malicious ratio on the main chain. Supports forms like 1/3 or 0.3333333333.",
    )
    parser.add_argument(
        "--epsilon",
        type=float,
        required=True,
        help="Acceptable failure probability.",
    )
    return parser.parse_args()


def validate_args(args):
    if args.total_nodes <= 0:
        raise ValueError("--total-nodes must be positive.")
    if not 0 <= args.malicious_ratio <= 1:
        raise ValueError("--malicious-ratio must be in [0, 1].")
    if not 0 <= args.threshold <= 1:
        raise ValueError("--threshold must be in [0, 1].")
    if not 0 <= args.epsilon <= 1:
        raise ValueError("--epsilon must be in [0, 1].")


def main():
    args = parse_args()
    validate_args(args)

    result = find_min_mainchain_size(
        total_nodes=args.total_nodes,
        malicious_ratio=args.malicious_ratio,
        malicious_threshold=args.threshold,
        epsilon=args.epsilon,
    )
    mainchain_size, mainchain_ratio, failure_prob, malicious_nodes = result

    print(f"total_nodes={args.total_nodes}")
    print(f"malicious_ratio={args.malicious_ratio} ({float(args.malicious_ratio):.6f})")
    print(f"malicious_nodes={malicious_nodes}")
    print(f"mainchain_threshold={args.threshold} ({float(args.threshold):.6f})")
    print(f"epsilon={args.epsilon:.6e}")

    if mainchain_size is None:
        print("No feasible main-chain size found under the given parameters.")
        return

    print(f"min_mainchain_size={mainchain_size}")
    print(f"min_mainchain_ratio={mainchain_ratio:.6%}")
    print(f"failure_prob_at_min_size={failure_prob:.6e}")


if __name__ == "__main__":
    main()
