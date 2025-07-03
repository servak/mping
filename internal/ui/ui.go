package ui

// UI は UI コンポーネントのインターフェース
type UI interface {
	Run() error
	Update()
	Close()
}
