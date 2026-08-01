package devices

import "fmt"

// Factory constructs a Device from Config. Each device class registers its
// own factory in init(), so adding a new class means adding a file here,
// not editing a central switch statement.
//
// This is a function type: Factory isn't a struct or interface, it's a name
// for "any function shaped like func(Config) Device". In Go, functions are
// values just like ints or strings, so anything with that exact signature —
// a named func, an anonymous func literal, a method value — can be assigned
// to a variable of type Factory, or in our case, stored as a map value.
type Factory func(cfg Config) Device

// map[Type]Factory declares a map from our Type enum (the key) to a
// Factory function (the value) — i.e. "given a device type, what function
// builds one?". The trailing {} is a composite literal: it creates an
// empty-but-initialized map, ready to read/write immediately. (A `var m
// map[K]V` with no {} would be nil, and writing to a nil map panics — the
// {} here is what avoids that.)
var registry = map[Type]Factory{}

// Register associates a Type with the Factory that builds it. Intended to
// be called from init() in each device class's file.
func Register(t Type, f Factory) {
	if _, exists := registry[t]; exists {
		panic(fmt.Sprintf("devices: duplicate registration for type %q", t))
	}
	registry[t] = f
}

// New builds a Device for cfg.Type using its registered Factory.
//
// Not a method — no receiver like `(p *basicPusher)` before the name, so
// this is just a regular package-level function, called as devices.New(cfg)
// from other packages. The (Device, error) return is Go's standard
// error-handling shape: return the result plus an error that's nil on
// success, and let the caller check `if err != nil` rather than throwing/
// catching exceptions.
func New(cfg Config) (Device, error) {
	f, ok := registry[cfg.Type]
	if !ok {
		return nil, fmt.Errorf("devices: no factory registered for type %q", cfg.Type)
	}
	return f(cfg), nil
}
