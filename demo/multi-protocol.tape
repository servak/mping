# Regenerate demo/multi-protocol.gif (run from the repo root):
#   go build -o mping ./cmd/mping/
#   sudo chown root mping && sudo chmod u+s mping   # once, so ICMP works without sudo below
#   vhs demo/multi-protocol.tape

Output demo/multi-protocol.gif

Set Shell "bash"
Set FontSize 15
Set Width 1900
Set Height 600
Set Padding 20
Set Theme "Dracula"
Set TypingSpeed 30ms

# ICMP, HTTPS, TCP, DNS and NTP checks side by side in one command
Type@10ms "./mping -i 500 192.168.1.1 https://google.com tcp://google.com:443 dns://8.8.8.8/google.com/A ntp://time.cloudflare.com"
Sleep 1500ms
Enter
Sleep 6s

# Details of the HTTPS check
Type "g"
Sleep 300ms
Type "j"
Sleep 300ms
Type "j"
Sleep 300ms
Type "v"
Sleep 3s
Type "v"
Sleep 500ms

# Cycle the sort key
Type "s"
Sleep 1500ms
Type "s"
Sleep 1500ms

Type "q"
Sleep 2s
