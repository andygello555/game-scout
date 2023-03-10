package api

import (
	"fmt"
	"reflect"
)

// BindingParam represents a param for a Binding. Binding.Execute uses BindingParam(s) for type-checking the arguments
// passed into it. To create a BindingParam use the available constructors:
//   - Param
//   - ReqParam
//   - Params
type BindingParam struct {
	// name is the name of the BindingParam.
	name string
	// variadic is whether the BindingParam is variadic. A BindingParam can only be variadic if it occurs at the very
	// end of a list of BindingParam(s), and there can only be one variadic BindingParam in each list of
	// BindingParam(s). variadic takes precedence over required, as variadic parameters are not required. If variadic is
	// set, then defaultValue must be an empty reflect.Slice/reflect.Array type.
	variadic bool
	// required is whether this BindingParam is required. Non-required BindingParam(s) can only occur at the end of a
	// list of required BindingParam(s), and before the variadic BindingParam (if there is one).
	required bool
	// defaultValue is the default value of a BindingParam if it is not required. If it is required then defaultValue is
	// used to find the type of this BindingParam for type-checking.
	defaultValue any
	// t is the type of the defaultValue, or the defaultValue's interface that will be set on the first call to Type, or
	// set when creating the BindingParam.
	t reflect.Type
	// interfaceFlag is set when the type denoted by t is an interface.
	interfaceFlag bool
}

func getReflectType(a any) (reflect.Type, bool, any) {
	switch a.(type) {
	case reflect.Type, reflect.Value:
		var t reflect.Type
		val := reflect.ValueOf(nil)
		switch a.(type) {
		case reflect.Value:
			val = a.(reflect.Value)
			t = val.Type()
		default:
			t = a.(reflect.Type)
		}

		// Check if the type of "a" is a pointer to an interface. If so, then we will return the reflect.Type of the
		// interface (sans indirection).
		if t.Kind() == reflect.Ptr && t.Elem().Kind() == reflect.Interface {
			// If the value is nil, then the default value is also nil
			if !val.IsValid() || val.IsZero() || val.IsNil() {
				return t.Elem(), true, nil
			}
			return t.Elem(), true, val.Elem().Interface()
		}

		if !val.IsValid() {
			return t, false, nil
		}
		return t, false, val.Interface()
	default:
		return reflect.TypeOf(a), false, a
	}
}

// String returns the string representation of the BindingParam in the format:
//
//	<name>: ["[I]" if interface]<type>["?" if !required]["..." if variadic][" = <defaultValue>" if !required]
func (bp BindingParam) String() string {
	required := ""
	def := ""
	if !bp.required {
		required = "?"
		switch bp.defaultValue.(type) {
		case string:
			def = fmt.Sprintf(" = %q", bp.defaultValue)
		default:
			def = fmt.Sprintf(" = %v", bp.defaultValue)
		}
	}

	i := ""
	if bp.interfaceFlag {
		i = "[I]"
	}

	variadic := ""
	if bp.variadic {
		variadic = "..."
	}
	return fmt.Sprintf("%s: %s%v%s%s%s", bp.name, i, bp.Type(), required, variadic, def)
}

// Type returns the reflect.Type of the BindingParam.
func (bp BindingParam) Type() reflect.Type {
	return bp.t
}

// Param returns a non-required BindingParam with the given name and default value. The required type for this
// BindingParam will be found using reflection on this default value.
func Param(name string, val any) BindingParam {
	t, interfaceFlag, defV := getReflectType(val)
	return BindingParam{
		name:          name,
		defaultValue:  defV,
		t:             t,
		interfaceFlag: interfaceFlag,
	}
}

// ReqParam returns a required BindingParam with the given name and type (reflected from the given value).
func ReqParam(name string, val any) BindingParam {
	t, interfaceFlag, defV := getReflectType(val)
	return BindingParam{
		name:          name,
		required:      true,
		defaultValue:  defV,
		t:             t,
		interfaceFlag: interfaceFlag,
	}
}

// VarParam returns a variadic BindingParam with the given name and type (reflected from the given value).
func VarParam(name string, val any) BindingParam {
	t, interfaceFlag, defV := getReflectType(val)
	return BindingParam{
		name:          name,
		required:      false,
		variadic:      true,
		defaultValue:  defV,
		t:             t,
		interfaceFlag: interfaceFlag,
	}
}

// Params constructs an array of BindingParam using the given arguments. The arguments will be treated as groupings of
// 2-4 values:
//  1. "name" (string): the name of the BindingParam.
//  2. "val" (any): the default value/type of the BindingParam. If this is reflect.Value or reflect.Type then the type
//     information will be taken from these. If you want a required BindingParam that checks for an interface value then
//     you will have to pass in a reflect.Value/reflect.Type of the pointer to the nil interface. For instance, if you
//     wanted to pass in a required Client instance to Binding.Execute: "reflect.TypeOf((*Client)(nil))". The type of the
//     Client interface will then be correctly stored in the BindingParam. However, if you wanted to pass in a
//     non-required Client instance to Binding.Execute then you would need to pass in a reflect.Value of a pointer to the
//     default value: "reflect.ValueOf(&client)" (where "client" is an instance of Client).
//  3. "required" (bool): whether this BindingParam is required. This is optional and will default to false.
//  4. "variadic" (bool): whether this BindingParam is variadic. This is also optional, but "required" must also be
//     given, otherwise the "variadic" argument is treated as the "required" argument. Defaults to false. This will also
//     treat "required" as false when given.
//
// If any of these groupings are incomplete/incorrect, then we will ignore that grouping. Each incomplete grouping will
// be ignored until the next instance of a string argument (i.e. the name of the next BindingParam). Be careful when
// there is BindingParam for a string/bool parameter. This is because Params might treat the "val" argument in the
// grouping as either the "name" or the "required" argument.
//
// Note: that Params will not be checked if they follow the rules described in the documentation for BindingParam. These
// rules are checked when Binding.Params or Binding.SetParamsMethod is called for a Binding, and the appropriate error
// is cached until Binding.Execute is called.
func Params(args ...any) []BindingParam {
	bindingParams := make([]BindingParam, 0)

	var (
		currentBinding    BindingParam
		currentBindingArg int
		t                 reflect.Type
		interfaceFlag     bool
		defVal            any
	)

	resetCurrentBinding := func() {
		currentBinding = BindingParam{}
		currentBindingArg = 0
	}

	setDefaultValue := func() {
		currentBinding.defaultValue = defVal
		currentBinding.t = t
		currentBinding.interfaceFlag = interfaceFlag
		// Add the param to the list of params. If there is an additional "required" argument after this one,
		// then, if a bool is up next, the reflect.Bool case will modify the BindingParam that was pushed to the
		// list.
		bindingParams = append(bindingParams, currentBinding)
		resetCurrentBinding()
		currentBindingArg = 2
	}

	// If skipUntilName is set, then we will keep skipping arguments until we get to a string argument. This is why you
	// should double-check whenever passing in a BindingParam argument grouping for a string default value.
	skipUntilName := false
	for _, arg := range args {
		t, interfaceFlag, defVal = getReflectType(arg)

		switch t.Kind() {
		case reflect.String:
			// If the "required" and or "variadic" argument cannot be found but this string one can then we will reset
			// the current binding. This will cause the program to set the recently reset param's name to be set as
			// well.
			if currentBindingArg == 2 || currentBindingArg == 3 {
				resetCurrentBinding()
			}

			if currentBindingArg == 0 {
				skipUntilName = false
				currentBinding.name = arg.(string)
				currentBindingArg++
				continue
			}

			// If the argument is a string, but we are on the second argument in the grouping, then the argument might
			// be for a string parameter. In which case, we can fallthrough the next case to handle the default value.
			fallthrough
		default:
			if skipUntilName {
				continue
			}

			if currentBindingArg == 1 {
				setDefaultValue()
			}
		case reflect.Bool:
			if skipUntilName {
				continue
			}

			lastBindingParamIdx := len(bindingParams) - 1
			switch currentBindingArg {
			case 1:
				setDefaultValue()
			case 2:
				bindingParams[lastBindingParamIdx].required = arg.(bool)
				currentBindingArg++
			case 3:
				// If Variadic is true, we will also make sure Required is unset.
				if bindingParams[lastBindingParamIdx].variadic = arg.(bool); bindingParams[lastBindingParamIdx].variadic {
					bindingParams[lastBindingParamIdx].required = false
				}
				resetCurrentBinding()
			default:
				// If we are on a bool that is not in pos 1 or 2 of the grouping then we don't know what to do with this
				// grouping, so we will skip until a name/string in the next grouping.
				skipUntilName = true
				resetCurrentBinding()
			}
		}
	}
	return bindingParams
}
