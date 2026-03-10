package bft

import (
	"Chamael/pkg/core"
	pkgcrypto "Chamael/pkg/crypto"
	"math/big"

	"go.dedis.ch/kyber/v3"
	"go.dedis.ch/kyber/v3/pairing"
	"go.dedis.ch/kyber/v3/sign/bls"
)

func cpuBLSSign(pid uint32, suite pairing.Suite, sk kyber.Scalar, msg []byte) ([]byte, error) {
	return core.WithCPULimitValueErr(pid, "bls_sign", func() ([]byte, error) {
		return bls.Sign(suite, sk, msg)
	})
}

func cpuBLSVerify(pid uint32, suite pairing.Suite, pk kyber.Point, msg, sig []byte) error {
	return core.WithCPULimitErr(pid, "bls_verify", func() error {
		return bls.Verify(suite, pk, msg, sig)
	})
}

func cpuBLSAggregateSignatures(pid uint32, suite pairing.Suite, sigs ...[]byte) ([]byte, error) {
	return core.WithCPULimitValueErr(pid, "bls_aggregate_sig", func() ([]byte, error) {
		return bls.AggregateSignatures(suite, sigs...)
	})
}

func cpuBLSAggregatePublicKeys(pid uint32, suite pairing.Suite, pks ...kyber.Point) kyber.Point {
	return core.WithCPULimitValue(pid, "bls_aggregate_pk", func() kyber.Point {
		return bls.AggregatePublicKeys(suite, pks...)
	})
}

func cpuFastAcc(pid uint32, set []string, encodeType pkgcrypto.EncodeType, setup *pkgcrypto.Setup) *big.Int {
	return core.WithCPULimitValue(pid, "fast_acc", func() *big.Int {
		return pkgcrypto.FastAcc(set, encodeType, setup)
	})
}

func cpuNewMerkleTree(pid uint32, data [][]string) (*pkgcrypto.MerkleTree, error) {
	return core.WithCPULimitValueErr(pid, "merkle_tree", func() (*pkgcrypto.MerkleTree, error) {
		return pkgcrypto.NewMerkleTree(data)
	})
}
