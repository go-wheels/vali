package vali

import "reflect"

type valueReference struct {
	typ reflect.Type
	ptr uintptr
	len int
}

func unpack(value any) any {
	rv := indirectValue(reflect.ValueOf(value))
	if !rv.IsValid() || isNilValue(rv) {
		return nil
	}
	return rv.Interface()
}

func indirectValue(rv reflect.Value) reflect.Value {
	var active map[valueReference]bool
	var first valueReference
	for rv.IsValid() {
		switch rv.Kind() {
		case reflect.Ptr:
			if rv.IsNil() {
				return reflect.Value{}
			}
			ref := valueReference{typ: rv.Type(), ptr: rv.Pointer()}
			if first.typ == nil {
				first = ref
			} else {
				if ref == first || active[ref] {
					return reflect.Value{}
				}
				if active == nil {
					active = make(map[valueReference]bool)
				}
				active[ref] = true
			}
			rv = rv.Elem()
		case reflect.Interface:
			rv = rv.Elem()
		default:
			return rv
		}
	}
	return rv
}

func isNilValue(rv reflect.Value) bool {
	return !rv.IsValid() || (canBeNil(rv.Kind()) && rv.IsNil())
}

func nestedStructType(typ reflect.Type) reflect.Type {
	seen := make(map[reflect.Type]bool)
	for typ != nil && !seen[typ] {
		seen[typ] = true
		switch typ.Kind() {
		case reflect.Ptr, reflect.Slice, reflect.Array:
			typ = typ.Elem()
		case reflect.Struct:
			return typ
		default:
			return nil
		}
	}
	return nil
}

func indirectType(typ reflect.Type) reflect.Type {
	seen := make(map[reflect.Type]bool)
	for typ.Kind() == reflect.Ptr {
		if seen[typ] {
			return nil
		}
		seen[typ] = true
		typ = typ.Elem()
	}
	return typ
}

func canBeNil(kind reflect.Kind) bool {
	switch kind {
	case reflect.Chan, reflect.Func, reflect.Map, reflect.Ptr, reflect.Slice, reflect.Interface:
		return true
	default:
		return false
	}
}
