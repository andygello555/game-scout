package models

import (
	"database/sql"
	"fmt"
	"gorm.io/gorm/callbacks"
	"time"
)

// NullDuration represents a nullable version of time.Duration.
type NullDuration struct {
	sql.NullInt64
}

// FromDurationPtr sets the referred to NullDuration to the given time.Duration, setting NullDuration.NullInt64.Valid
// appropriately.
func (nd *NullDuration) FromDurationPtr(duration *time.Duration) {
	*nd = NullDuration{
		sql.NullInt64{},
	}
	nd.NullInt64.Valid = duration != nil
	if nd.NullInt64.Valid {
		nd.NullInt64.Int64 = int64(*duration)
	}
}

func (nd *NullDuration) IsValid() bool {
	return nd.Valid
}

func (nd *NullDuration) Ptr() *time.Duration {
	if !nd.IsValid() {
		return nil
	}
	duration := time.Duration(nd.NullInt64.Int64)
	return &duration
}

// NullDurationFrom constructs a new NullDuration from the given time.Duration. This will never be null.
func NullDurationFrom(duration time.Duration) NullDuration {
	nd := NullDuration{}
	nd.FromDurationPtr(&duration)
	return nd
}

// NullDurationFromPtr constructs a new NullDuration from the given pointer to a time.Duration.
func NullDurationFromPtr(duration *time.Duration) NullDuration {
	nd := NullDuration{}
	nd.FromDurationPtr(duration)
	return nd
}

type WeightedModel interface {
	callbacks.BeforeCreateInterface
	callbacks.BeforeUpdateInterface
}

type CheckedWeightedModel interface {
	WeightedModel
	CheckCalculateWeightedScore() bool
}

type WeightedField interface {
	fmt.Stringer
	Weight() (w float64, inverse bool)
	GetValueFromWeightedModel(model WeightedModel) []float64
	// Fields returns all the WeightedField that make up the set of WeightedField
	Fields() []WeightedField
}

func CalculateWeightedScore(model WeightedModel, singleField WeightedField) float64 {
	weightSum := 0.0
	weightFactorSum := 0.0
	// For each weighted field we will fetch its float64 values, sum these values, then calculate the weighted value using
	// the weight factor retrieved from the WeightedField.
	for _, field := range singleField.Fields() {
		//log.WARNING.Printf("Calculating weighted value for %s.%s", reflect.TypeOf(model).Elem().String(), field.String())
		weightFactor, inverse := field.Weight()
		weightFactorSum += weightFactor
		valueSum := 0.0
		for _, value := range field.GetValueFromWeightedModel(model) {
			valueSum += value
		}
		// We inverse the valueSum if this field requires it
		if inverse && valueSum != 0.0 {
			valueSum = 1 / valueSum
		}
		//log.WARNING.Printf("\tValue of %s = %f, inverse = %t", field.String(), valueSum, inverse)
		//fmt.Printf("field: %s, value: %f, factor: %f, weightedValue: %f, sum: %f\n", field.String(), valueSum, weightFactor, valueSum*weightFactor, weightSum)
		// Then we calculate the weighted value and add it to the sum of the weighted values
		weightSum += valueSum * weightFactor
		//log.WARNING.Printf("\tWeighted value of %s = %f, weightSum = %f", field.String(), valueSum*weightFactor, weightSum)
	}
	//log.WARNING.Printf("Weighted score for %s = %f", reflect.TypeOf(model).Elem().String(), weightSum/weightFactorSum)
	return weightSum / weightFactorSum
}
