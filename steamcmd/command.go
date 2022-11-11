package steamcmd

import (
	"fmt"
	"github.com/pkg/errors"
	"reflect"
	"strconv"
)

type CommandType int

const (
	AppInfoPrint CommandType = iota
	Quit
)

func (ct CommandType) String() string {
	switch ct {
	case AppInfoPrint:
		return "app_info_print"
	case Quit:
		return "quit"
	default:
		return "<nil>"
	}
}

type ArgType int

const (
	Number ArgType = iota
	String
)

func (at ArgType) String() string {
	switch at {
	case Number:
		return "Number"
	case String:
		return "String"
	default:
		return "<nil>"
	}
}

// DefaultSerialiser serialises the given value to a string using the default logic for the ArgType.
func (at ArgType) DefaultSerialiser(value any) string {
	switch at {
	case Number:
		switch value.(type) {
		case int, int8, int16, int32, int64:
			v := reflect.ValueOf(value)
			return strconv.Itoa(int(v.Int()))
		case float32, float64:
			v := reflect.ValueOf(value)
			return fmt.Sprintf("%f", v.Float())
		default:
			panic(errors.Errorf(
				"cannot serialise a %s that has the value %v (type: %s)",
				at.String(), value, reflect.TypeOf(value).String()),
			)
		}
	case String:
		return value.(string)
	default:
		return "<nil>"
	}
}

type ArgValidator func(any) bool
type ArgSerialiser func(any) string

type Arg struct {
	Name       string
	Type       ArgType
	Required   bool
	Validator  ArgValidator
	Serialiser ArgSerialiser
}

func (a *Arg) Serialise(val any) string {
	if a.Serialiser != nil {
		return a.Serialiser(val)
	}
	return a.Type.DefaultSerialiser(val)
}

type CommandOutputValidator func(string) bool
type CommandOutputParser func([]byte) any

type Command struct {
	Type      CommandType
	Parser    CommandOutputParser
	Validator CommandOutputValidator
}
