package require

import "reflect"

func NotNil(name string, value any) {
	if value == nil {
		panic(name + " is nil")
	}

	reflectValue := reflect.ValueOf(value)
	switch reflectValue.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if reflectValue.IsNil() {
			panic(name + " is nil")
		}
	}
}
