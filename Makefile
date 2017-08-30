bench:
	go test -v -bench=. -benchtime=10ms

benchlong:
	go test -v -bench=. -benchtime=2s

benchprofile:
	go test -bench=BenchmarkRegexpBypass -benchtime=10s -cpuprofile=cpuprofile.prof -memprofile=memprofile.mprof
	go tool pprof regexp-bypass.test cpuprofile.prof || true
	go tool pprof regexp-bypass.test memprofile.mprof || true
	rm cpuprofile.prof
	rm memprofile.mprof
	rm regexp-bypass.test

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
