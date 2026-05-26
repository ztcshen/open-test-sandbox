package sqlstore

type scanner interface {
	Scan(dest ...any) error
}
