# Regenerate demo/icmp.gif (run from the repo root):
#   go build -o mping ./cmd/mping/
#   sudo chown root mping && sudo chmod u+s mping   # once, so ICMP works without sudo below
#   vhs demo/icmp.tape

Output demo/icmp.gif

Set Shell "bash"
Set FontSize 15
Set Width 1900
Set Height 680
Set Padding 20
Set Theme "Dracula"
Set TypingSpeed 30ms

# History column and outage timer (192.0.2.1 never answers, so it stays DOWN)
Type "./mping -i 100 192.168.1.1 8.8.8.8 1.1.1.1 github.com 192.0.2.1"
Sleep 1s
Enter
Sleep 4s

# Host details of the DOWN host: outage count, downtime, longest outage
Type "g"
Sleep 300ms
Type "v"
Sleep 3500ms
Type "v"
Sleep 500ms

# Quitting prints the outage timeline
Type "q"
Sleep 3s
