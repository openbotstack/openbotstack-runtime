//go:build tinygo.wasm

package main

import "unsafe"

// Host API functions (imported from 'env' module)
//
//go:wasmimport env get_input_len
func get_input_len() uint32

//go:wasmimport env get_input
func get_input(ptr uint32) uint32

//go:wasmimport env set_output
func set_output(ptr uint32, length uint32)

//go:wasmexport execute
func wasmExecute() {
	// 1. Read input via Host API
	inLen := get_input_len()
	if inLen == 0 {
		return
	}

	inputData := make([]byte, inLen)
	get_input(uint32(uintptr(unsafe.Pointer(&inputData[0]))))

	// 2. Run calculation
	result := calculateTax(inputData)
	if result == nil {
		return
	}

	// 3. Write output via Host API
	set_output(uint32(uintptr(unsafe.Pointer(&result[0]))), uint32(len(result)))
}
