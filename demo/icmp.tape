# Regenerate demo/mping.gif (run from the repo root):
#   go build -o mping ./cmd/mping/
#   sudo chown root mping && sudo chmod u+s mping   # once, so ICMP works without sudo below
#   vhs demo/demo.tape

Output demo/mping.gif

Set Shell "bash"
Set FontSize 16
Set Width 1500
Set Height 560
Set Padding 20
Set Theme "Dracula"
Set TypingSpeed 30ms

# Subnet expansion (CIDR notation expands to individual ICMP targets)
Type "./mping -i 100 192.168.1.1 8.8.8.8 github.com 192.168.1.108/31"
Sleep 1s
Enter
Sleep 2s
Type "v"
Sleep 2s
Type "j"
Sleep 1s
Type "j"
Sleep 1s
Type "j"
Sleep 2s

Type "q"
Sleep 1s

# Multi-protocol monitoring
Type@10ms "./mping 192.168.1.1 https://google.com tcp://google.com:443 dns://8.8.8.8/google.com/A ntp://time.cloudflare.com"
Sleep 1500ms
Enter
Sleep 1s
Type "v"
Sleep 3s
Type "j"
Sleep 1s
Type "j"
Sleep 1s
Type "j"
Sleep 1s

Type "s"
Sleep 1500ms
Type "s"
Sleep 1500ms

Type "q"
Sleep 1s
