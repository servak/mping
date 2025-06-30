#!/bin/sh
set -e

case "$1" in
    configure)
        # Set the CAP_NET_RAW capability on the binary
        if [ -f /usr/local/bin/mping ]; then
            setcap 'cap_net_raw+p' /usr/local/bin/mping || {
                echo "Warning: Failed to set cap_net_raw capability on /usr/local/bin/mping"
                echo "The application may need to run as root for ICMP operations"
                echo "To fix this manually, run: sudo setcap cap_net_raw=+ep /usr/local/bin/mping"
                exit 0
            }
            echo "Successfully set cap_net_raw capability on mping"
        else
            echo "Warning: mping binary not found at /usr/local/bin/mping"
        fi
        ;;
    *)
        ;;
esac

exit 0
