bench:
	go test -bench=. -benchtime=10ms

benchlong:
	go test -bench=. -benchtime=2s

test:
	go test ./regexp -v

testmatch:
	go test ./regexp -v -run=TestMatch

testfind:
	go test ./regexp -v -run=TestFind

testbypass:
	go test ./regexp -v -run=TestByPass*

chart:
	go run benchmark_chart/main.go
	open benchmark_chart/output.png

fuzz:
	rm -rf fuzzdata/
	rm -rf ./regexpbypass-fuzz.zip
	mkdir -p fuzzdata
	go-fuzz-build github.com/sylvinus/regexp-bypass
	make continuefuzz

continuefuzz:
	go-fuzz -bin=./regexpbypass-fuzz.zip -workdir=fuzzdata

lint:
	go fmt *.go
	go fmt ./regexp/*.go
	go fmt ./benchmark_chart/*.go
	aligncheck ./regexp