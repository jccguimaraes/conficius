package shell

type Shell interface {
	RunCommand() error
}
