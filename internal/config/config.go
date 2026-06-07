package config

type Snapshot struct {
	Platform Platform
	UserEnv  UserEnv
	Shell    Shell
}

func Current() (*Snapshot, error) {
	platform := ReadPlatform()

	userenv, err := ReadUserEnv()
	if err != nil {
		return nil, err
	}

	shell, err := ReadShell()
	if err != nil {
		return nil, err
	}

	snapshot := &Snapshot{
		Platform: platform,
		UserEnv:  userenv,
		Shell:    shell,
	}

	return snapshot, nil
}
