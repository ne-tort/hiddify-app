package main
import ("encoding/json"; "fmt")
type Inner struct { GroupIds []uint `json:"group_ids,omitempty"` }
type Outer struct {
	Inner
	GroupIds *[]uint `json:"group_ids"`
}
func main() {
	j := []byte(`{"group_ids":[1,2,3]}`)
	var o Outer
	json.Unmarshal(j, &o)
	fmt.Printf("Outer.GroupIds ptr: %v\n", o.GroupIds)
	fmt.Printf("Inner.GroupIds: %v\n", o.Inner.GroupIds)
}
