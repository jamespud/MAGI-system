package inputartifact

import (
	"github.com/jamespud/magi/backend/domain/canonical"
	"math/big"
)

func safeUint(v int64) bool { return v >= 0 && v <= canonical.MaxSafeInteger }
func validateQuantity(q Quantity, path string) error {
	if !safeUint(q.Value) || q.Scale < 0 || q.Scale > 9 {
		return invalid("QUANTITY", path, "unsafe value or scale")
	}
	cap := int64(1)
	for i := int64(0); i < q.Scale; i++ {
		cap *= 10
	}
	switch q.Unit {
	case "ratio":
		if q.Value > cap {
			return invalid("QUANTITY", path+".value", "ratio must be within [0,1]")
		}
	case "percentage_point":
		if q.Value > 100*cap {
			return invalid("QUANTITY", path+".value", "percentage points must be within [0,100]")
		}
	case "millisecond", "token", "tool_call", "byte", "count":
		if q.Scale != 0 {
			return invalid("QUANTITY", path+".scale", "discrete quantities require scale 0")
		}
	default:
		return invalid("QUANTITY", path+".unit", "unknown unit")
	}
	return nil
}
func quantityRat(q Quantity) *big.Rat {
	return new(big.Rat).SetFrac(big.NewInt(q.Value), new(big.Int).Exp(big.NewInt(10), big.NewInt(q.Scale), nil))
}

// CompareQuantities uses arbitrary precision for intermediate cross products.
func CompareQuantities(a, b Quantity) (int, error) {
	if e := validateQuantity(a, "$.left"); e != nil {
		return 0, e
	}
	if e := validateQuantity(b, "$.right"); e != nil {
		return 0, e
	}
	if a.Unit != b.Unit {
		return 0, invalid("QUANTITY", "$.right.unit", "incompatible units")
	}
	return quantityRat(a).Cmp(quantityRat(b)), nil
}
func validateRational(r Rational, path string) error {
	if !safeUint(r.Numerator) || !safeUint(r.Denominator) || r.Denominator == 0 || r.Numerator > r.Denominator {
		return invalid("RATIONAL", path, "invalid exact probability")
	}
	if new(big.Int).GCD(nil, nil, big.NewInt(r.Numerator), big.NewInt(r.Denominator)).Cmp(big.NewInt(1)) != 0 {
		return invalid("RATIONAL", path, "fraction must be reduced")
	}
	return nil
}

// PerClaimAlpha returns an exact reduced split, refusing unencodable results.
func PerClaimAlpha(family Rational, claims int64) (Rational, error) {
	if e := validateRational(family, "$.family_alpha"); e != nil {
		return Rational{}, e
	}
	if claims <= 0 || !safeUint(claims) {
		return Rational{}, invalid("RATIONAL", "$.claim_count", "positive safe claim count required")
	}
	r := new(big.Rat).Quo(new(big.Rat).SetFrac(big.NewInt(family.Numerator), big.NewInt(family.Denominator)), new(big.Rat).SetInt64(claims))
	if !r.Num().IsInt64() || !r.Denom().IsInt64() || !safeUint(r.Num().Int64()) || !safeUint(r.Denom().Int64()) {
		return Rational{}, invalid("PRECISION_OVERFLOW", "$.family_alpha", "exact per-claim value exceeds safe encoding")
	}
	return Rational{Numerator: r.Num().Int64(), Denominator: r.Denom().Int64()}, nil
}
func validateMoney(m Money, path string) error {
	if !safeUint(m.MinorUnits) || m.MinorUnitExponent < 0 || m.MinorUnitExponent > 9 || len(m.Currency) != 3 {
		return invalid("MONEY", path, "invalid minor units, exponent or currency")
	}
	for _, c := range m.Currency {
		if c < 'A' || c > 'Z' {
			return invalid("MONEY", path+".currency", "currency must use three uppercase letters")
		}
	}
	return nil
}
