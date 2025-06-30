build: clean
	go build ./cmd/mping

clean:
	sudo rm -f mping

setcap: build
	sudo chown root mping
	sudo chmod u+s mping
