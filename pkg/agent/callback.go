package agent

type Callback struct {
	OnDisconnected func()
}

func NewCallback() *Callback {
	return &Callback{
		OnDisconnected: func() {},
	}
}
