# DEPRECATED

Lunanode installer is deprecated and will be removed in the next bitcartcc major version: 0.7.0.0

Use [BitcartCC configurator](https://docs.bitcartcc.com/guides/configurator) instead

# Launch BitcartCC on lunanode

This is the code for bitcartcc launcher at https://launch.bitcartcc.com

To run, simply:

`go run main.go`

When a user requests to provision a VM, the webserver will read `run.sh` and
replace parameters with user-specified configuration. The resulting script will
be passed as a startup script to the VM, via cloud-init.
