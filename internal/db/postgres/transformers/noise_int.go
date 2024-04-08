// Copyright 2023 Greenmask
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package transformers

import (
	"context"
	"fmt"

	"github.com/greenmaskio/greenmask/internal/db/postgres/transformers/utils"
	"github.com/greenmaskio/greenmask/internal/generators/transformers"
	"github.com/greenmaskio/greenmask/pkg/toolkit"
)

var NoiseIntTransformerDefinition = utils.NewTransformerDefinition(
	utils.NewTransformerProperties(
		"NoiseInt",
		"Make noise value for int",
	),

	NewNoiseIntTransformer,

	toolkit.MustNewParameterDefinition(
		"column",
		"column name",
	).SetIsColumn(toolkit.NewColumnProperties().
		SetAffected(true).
		SetAllowedColumnTypes("int2", "int4", "int8").
		SetSkipOnNull(true),
	).SetRequired(true),

	toolkit.MustNewParameterDefinition(
		"min_ratio",
		"max random percentage for noise",
	).SetDefaultValue(toolkit.ParamsValue("0.2")),

	toolkit.MustNewParameterDefinition(
		"max_ratio",
		"max random percentage for noise",
	).SetDefaultValue(toolkit.ParamsValue("0.05")),

	toolkit.MustNewParameterDefinition(
		"min",
		"min value threshold limiter",
	).SetDynamicMode(
		toolkit.NewDynamicModeProperties().
			SetCompatibleTypes("int2", "int4", "int8"),
	),

	toolkit.MustNewParameterDefinition(
		"max",
		"max value threshold limiter",
	).SetDynamicMode(
		toolkit.NewDynamicModeProperties().
			SetCompatibleTypes("int2", "int4", "int8"),
	),

	engineParameterDefinition,
)

type NoiseIntTransformer struct {
	t               *transformers.NoiseInt64Transformer
	columnName      string
	columnIdx       int
	affectedColumns map[int]string
	intSize         int
	dynamicMode     bool

	columnParam   toolkit.Parameterizer
	maxRatioParam toolkit.Parameterizer
	minRatioParam toolkit.Parameterizer
	maxParam      toolkit.Parameterizer
	minParam      toolkit.Parameterizer
	engineParam   toolkit.Parameterizer

	transform func(context.Context, int64) (int64, error)
}

func NewNoiseIntTransformer(ctx context.Context, driver *toolkit.Driver, parameters map[string]toolkit.Parameterizer) (utils.Transformer, toolkit.ValidationWarnings, error) {
	var columnName, engine string
	var minRatio, maxRatio float64
	var maxValueThreshold, minValueThreshold int64
	var intSize int
	var dynamicMode bool

	columnParam := parameters["column"]
	maxRatioParam := parameters["min_ratio"]
	minRatioParam := parameters["max_ratio"]
	maxParam := parameters["min"]
	minParam := parameters["max"]
	engineParam := parameters["engine"]

	if err := engineParam.Scan(&engine); err != nil {
		return nil, nil, fmt.Errorf(`unable to scan "engine" param: %w`, err)
	}

	if err := columnParam.Scan(&columnName); err != nil {
		return nil, nil, fmt.Errorf("unable to scan column param: %w", err)
	}

	idx, c, ok := driver.GetColumnByName(columnName)
	if !ok {
		return nil, nil, fmt.Errorf("column with name %s is not found", columnName)
	}
	affectedColumns := make(map[int]string)
	affectedColumns[idx] = columnName

	if c.Length != -1 {
		intSize = c.Length
	}

	if !dynamicMode {
		if err := minParam.Scan(&maxValueThreshold); err != nil {
			return nil, nil, fmt.Errorf("error scanning \"min\" parameter: %w", err)
		}
		if err := maxParam.Scan(&minValueThreshold); err != nil {
			return nil, nil, fmt.Errorf("error scanning \"max\" parameter: %w", err)
		}
	}

	if err := minRatioParam.Scan(&minRatio); err != nil {
		return nil, nil, fmt.Errorf("unable to scan \"min_ratio\" param: %w", err)
	}

	if err := maxRatioParam.Scan(&maxRatio); err != nil {
		return nil, nil, fmt.Errorf("unable to scan \"max_ratio\" param: %w", err)
	}

	limiter, limitsWarnings, err := validateIntTypeAndSetNoiseInt64Limiter(intSize, maxValueThreshold, minValueThreshold)
	if err != nil {
		return nil, nil, err
	}
	if limitsWarnings.IsFatal() {
		return nil, limitsWarnings, nil
	}

	t, err := transformers.NewNoiseInt64Transformer(limiter, minRatio, maxRatio)
	if err != nil {
		return nil, nil, fmt.Errorf("error initializing common int transformer: %w", err)
	}

	g, err := getGenerateEngine(ctx, engine, t.GetRequiredGeneratorByteLength())
	if err != nil {
		return nil, nil, fmt.Errorf("unable to get generator: %w", err)
	}
	if err = t.SetGenerator(g); err != nil {
		return nil, nil, fmt.Errorf("unable to set generator: %w", err)
	}

	return &NoiseIntTransformer{
		t:               t,
		columnName:      columnName,
		affectedColumns: affectedColumns,
		columnIdx:       idx,
		intSize:         intSize,

		columnParam:   columnParam,
		minParam:      minParam,
		maxParam:      maxParam,
		minRatioParam: minRatioParam,
		maxRatioParam: maxRatioParam,
		engineParam:   engineParam,
		transform:     t.Transform,
	}, nil, nil
}

func (nit *NoiseIntTransformer) GetAffectedColumns() map[int]string {
	return nit.affectedColumns
}

func (nit *NoiseIntTransformer) Init(ctx context.Context) error {
	return nil
}

func (nit *NoiseIntTransformer) Done(ctx context.Context) error {
	if nit.dynamicMode {
		nit.transform = nit.dynamicTransform
	}
	return nil
}

func (nit *NoiseIntTransformer) dynamicTransform(ctx context.Context, v int64) (int64, error) {
	minVal, maxVal, err := getMinAndMaxIntDynamicValueNoiseIntTrans(nit.intSize, nit.minParam, nit.maxParam)
	if err != nil {
		return 0, fmt.Errorf("unable to get min and max values: %w", err)
	}

	limiter, err := transformers.NewNoiseInt64Limiter(minVal, maxVal)
	if err != nil {
		return 0, fmt.Errorf("error creating limiter in dynamic mode: %w", err)
	}
	ctx = context.WithValue(ctx, "limiter", limiter)
	res, err := nit.t.Transform(ctx, v)
	if err != nil {
		return 0, fmt.Errorf("error generating int value: %w", err)
	}
	return res, nil
}

func (nit *NoiseIntTransformer) Transform(ctx context.Context, r *toolkit.Record) (*toolkit.Record, error) {
	var val int64
	isNull, err := r.ScanColumnValueByIdx(nit.columnIdx, &val)
	if err != nil {
		return nil, fmt.Errorf("unable to scan value: %w", err)
	}
	if isNull {
		return r, nil
	}

	res, err := nit.transform(ctx, val)
	if err != nil {
		return nil, fmt.Errorf("unable to transform value: %w", err)
	}

	if err = r.SetColumnValueByIdx(nit.columnIdx, res); err != nil {
		return nil, fmt.Errorf("unable to set new value: %w", err)
	}
	return r, nil
}

func validateIntTypeAndSetNoiseInt64Limiter(
	size int, requestedMinValue, requestedMaxValue int64,
) (limiter *transformers.NoiseInt64Limiter, warns toolkit.ValidationWarnings, err error) {

	minValue, maxValue, warns, err := validateInt64AndGetLimits(size, requestedMinValue, requestedMaxValue)
	if err != nil {
		return nil, nil, err
	}
	if warns.IsFatal() {
		return nil, warns, nil
	}
	l, err := transformers.NewNoiseInt64Limiter(minValue, maxValue)
	if err != nil {
		return nil, nil, err
	}
	return l, nil, nil
}

func getMinAndMaxIntDynamicValueNoiseIntTrans(intSize int, minParam, maxParam toolkit.Parameterizer) (int64, int64, error) {

	var requestedMinValue, requestedMaxValue int64
	var minRequested, maxRequested bool
	minValue, maxValue, err := getIntThresholds(intSize)
	if err != nil {
		return 0, 0, err
	}

	if minParam.IsDynamic() {
		minRequested = true
		err = minParam.Scan(&requestedMinValue)
		if err != nil {
			return 0, 0, fmt.Errorf(`unable to scan "min" dynamic  param: %w`, err)
		}
		if !limitIsValid(requestedMinValue, minValue, maxValue) {
			return 0, 0, fmt.Errorf("requested dynamic parameter min value is out of range of int%d size", intSize)
		}
	}

	if maxParam.IsDynamic() {
		maxRequested = true
		err = minParam.Scan(&maxValue)
		if err != nil {
			return 0, 0, fmt.Errorf(`unable to scan "max" dynamic param: %w`, err)
		}
		if !limitIsValid(requestedMaxValue, minValue, maxValue) {
			return 0, 0, fmt.Errorf("requested dynamic parameter max value is out of range of int%d size", intSize)
		}
	}

	if minRequested {
		minValue = requestedMinValue
	}
	if maxRequested {
		maxValue = requestedMaxValue
	}

	return minValue, maxValue, nil
}

func init() {
	utils.DefaultTransformerRegistry.MustRegister(NoiseIntTransformerDefinition)
}
