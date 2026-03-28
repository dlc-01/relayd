package notify

type Notifier interface {
	ClientConnected(label, tunnels, remote string)
	ClientDisconnected(label, remote string)
	InvalidToken(remote string)
	ServerStarted(addr string)
	Enabled() bool
}

type MultiNotifier struct {
	notifiers []Notifier
}

func NewMulti(notifiers ...Notifier) *MultiNotifier {
	return &MultiNotifier{notifiers: notifiers}
}

func (m *MultiNotifier) Enabled() bool {
	for _, n := range m.notifiers {
		if n.Enabled() {
			return true
		}
	}
	return false
}

func (m *MultiNotifier) ClientConnected(label, tunnels, remote string) {
	for _, n := range m.notifiers {
		n.ClientConnected(label, tunnels, remote)
	}
}

func (m *MultiNotifier) ClientDisconnected(label, remote string) {
	for _, n := range m.notifiers {
		n.ClientDisconnected(label, remote)
	}
}

func (m *MultiNotifier) InvalidToken(remote string) {
	for _, n := range m.notifiers {
		n.InvalidToken(remote)
	}
}

func (m *MultiNotifier) ServerStarted(addr string) {
	for _, n := range m.notifiers {
		n.ServerStarted(addr)
	}
}

type NoopNotifier struct{}

func (n *NoopNotifier) Enabled() bool                                 { return false }
func (n *NoopNotifier) ClientConnected(label, tunnels, remote string) {}
func (n *NoopNotifier) ClientDisconnected(label, remote string)       {}
func (n *NoopNotifier) InvalidToken(remote string)                    {}
func (n *NoopNotifier) ServerStarted(addr string)                     {}
