package loopback

// Logger interface closely related to the standard library log.Logger
// Only includes Print, Printf, Println, Fatal, Fatalf, Fatalln, Panic, Panicf, Panicln
// See: https://pkg.go.dev/log#Logger
type Logger interface {
	Print(v ...interface{})
	Printf(format string, v ...interface{})
	Println(v ...interface{})
	Fatal(v ...interface{})
	Fatalf(format string, v ...interface{})
	Fatalln(v ...interface{})
	Panic(v ...interface{})
	Panicf(format string, v ...interface{})
	Panicln(v ...interface{})
}
