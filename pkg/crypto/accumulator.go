package crypto

import (
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"strings"
)

// Setup stores RSA accumulator parameters.
type Setup struct {
	N *big.Int
	G *big.Int
	H *big.Int
}

// EncodeType defines how representatives are generated.
type EncodeType int

const (
	HashToPrimeFromSha256 EncodeType = iota
	// Other encoding modes such as DIHashFromPoseidon were removed.
)

// Constants preserved from the original 2048-bit setup strings.
const (
	RSABitLength = 2048
	N2048String  = "22582513446883649683242153375773765418277977026848618150278436227443969113525388360965414596382292671632010154272027792498289390464326093128963474525925743125404187090638221587455285089494562751793489098182761320953828657439130044252338283109583198301789045090284695934345711523245381620643226632165168827411546661236460973389982263385406789443858985073091473529732325356098830825299275985202060852102775942940039443155227986748457261585440368528834910182851433705587223040610934954417065434756145769875043620201897615075786323297141320586481340831246603933018654794846594742280842668198512719618188992528830140149361"
	G2048String  = "3734320578166922768976307305081280303658237303482921793243310032002132951325426885895423150554487167609218974062079302792001919827304933109188668552532361245089029380294384169787606911401094856511916709999954764232948323779503820860893459514928713744983707360078264267038900798843893405664990521531326919997106338139056096176409033756102908667173913246197068450150318832809948977367751025873698025220766782003611956130604742644746610708520581969538416206455665972248047959779079118036299417601968576259426648158714614452861031491553305187113545916330322686053758561416773919173504690956803771722726889946697788319929"
	// H is unused in the demo, so keep it as a simple constant.
	H2048String = "2"
)

// TrustedSetup returns 2048-bit RSA accumulator parameters.
// Note: demo only; do not use fixed constants in production.
func TrustedSetup() *Setup {
	ret := &Setup{
		N: new(big.Int),
		G: new(big.Int),
		H: new(big.Int),
	}
	ret.N.SetString(N2048String, 10)
	ret.G.SetString(G2048String, 10)
	ret.H.SetString(H2048String, 10)
	return ret
}

// AccAndProve generates representatives, computes membership proofs, and builds the accumulator.
func AccAndProve(set []string, encodeType EncodeType, setup *Setup) (*big.Int, []*big.Int) {
	reps := GenRepresentatives(set, encodeType)
	proofs := ProveMembership(setup.G, setup.N, reps)
	// Build the accumulator from proofs[0] and its representative.
	acc := AccumulateNew(proofs[0], reps[0], setup.N)
	return acc, proofs
}

// AccWithoutProve computes only the accumulator without proofs.
func AccWithoutProve(set []string, encodeType EncodeType, setup *Setup) (*big.Int, []*big.Int) {
	reps := GenRepresentatives(set, encodeType)
	prod := big.NewInt(1)
	for _, r := range reps {
		prod.Mul(prod, r)
	}
	acc := AccumulateNew(setup.G, prod, setup.N)
	return acc, reps
}

// FastAcc builds an accumulator quickly.
func FastAcc(set []string, encodeType EncodeType, setup *Setup) *big.Int {
	// Hash all set elements into one digest.
	hash := sha256.Sum256([]byte(strings.Join(set, "")))

	acc, _ := AccWithoutProve([]string{hex.EncodeToString(hash[:])}, encodeType, setup)
	return acc
}

// AccumulateNew computes base^exp mod N.
func AccumulateNew(base, exp, N *big.Int) *big.Int {
	return new(big.Int).Exp(base, exp, N)
}

// ProveMembership computes a membership proof for each representative.
// For each representative r, proof = g^((product of all representatives) / r) mod N.
func ProveMembership(g, N *big.Int, reps []*big.Int) []*big.Int {
	proofs := make([]*big.Int, len(reps))
	prod := big.NewInt(1)
	for _, r := range reps {
		prod.Mul(prod, r)
	}
	for i, r := range reps {
		quotient := new(big.Int).Div(prod, r)
		proofs[i] = new(big.Int).Exp(g, quotient, N)
	}
	return proofs
}

// GenRepresentatives generates representatives for the input set with the selected encoding.
func GenRepresentatives(set []string, encodeType EncodeType) []*big.Int {
	switch encodeType {
	case HashToPrimeFromSha256:
		return genRepWithHashToPrimeFromSHA256(set)
		// Other encoding modes were removed.
	default:
		return genRepWithHashToPrimeFromSHA256(set)
	}
}

// genRepWithHashToPrimeFromSHA256 calls HashToPrime for each set element.
func genRepWithHashToPrimeFromSHA256(set []string) []*big.Int {
	reps := make([]*big.Int, len(set))
	for i, v := range set {
		reps[i] = HashToPrime([]byte(v))
	}
	return reps
}

// HashToPrime hashes input data with SHA256 and finds the next prime not smaller than the hash.
func HashToPrime(data []byte) *big.Int {
	hash := sha256.Sum256(data)
	n := new(big.Int).SetBytes(hash[:])
	// Ensure n is prime.
	for !n.ProbablyPrime(20) {
		n.Add(n, big.NewInt(1))
	}
	return n
}
