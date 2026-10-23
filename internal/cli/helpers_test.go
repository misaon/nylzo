package cli

type noTerminal struct{}

func (noTerminal) StdinIsTerminal() bool { return false }

func (noTerminal) ReadSecret(string) ([]byte, error) { return nil, ErrNoTerminal }
