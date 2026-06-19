package main
import "github.com/valkey-io/valkey-go"
func main() {
    var c valkey.Completed
    _ = c.IsReadOnly()
}
