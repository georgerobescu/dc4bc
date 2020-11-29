# Preparing for the testnet dkg ceremony

## Prepare the hardware

To do things properly, you'll need two machines (either Linux or OS X ones). One should have internet access ("client"), and the other would be working offline ("airgapped"). Both machines should have webcams attached, as transactions and their signed versions would be transmitted via qr-codes

## Install the dependencies

You'll need to prepare `go` to build and use dkg apps. To get it ready, check out the `installation` guide here: https://github.com/lidofinance/dc4bc/blob/master/HowTo.md#step-by-step-guide

## Build the cli apps

Clone the git repo https://github.com/lidofinance/dc4bc and build the app for your client and airgapped machines following guides for their OS

**Note**: further in this guide we'll refer to `dc4bc_...` apps. You should run `dc4bc_..._linux` if you've built apps on Linux or `dc4bc_..._darwin` if you're on OS X

## Generate keypairs

You'll need to use apps to generate keypairs. Public keys from those keypair should be submitted to Lido's repo on the next step.

**NOTE:** if you've used the app versions with opencv, please, make sure to use fresh forlders for the `--key_store_dbdsn`, `--state_dbdsn`, `--db_path` parameters.

Run the command `gen_keys` on the client machine:
```
./dc4bc_d gen_keys --username john_doe --key_store_dbdsn /tmp/dc4bc_john_doe_key_store
```
Here:
1) Username is your desired username (one you'll be submitting to the list [here](https://github.com/lidofinance/dc4bc-conference-call/blob/master/dc4bc-conference-call/dc4bc-async-ceremony-27-11-2020.json))

Keypair for the airgapped machine is generated upon the first launch

## Start the node communicating with Kafka for testnet

On the client machine, run the command `start`:
```
./dc4bc_d start --username john_doe --key_store_dbdsn /tmp/dc4bc_john_doe_key_store --listen_addr localhost:8080 --state_dbdsn /tmp/dc4bc_john_doe_state --storage_dbdsn 51.158.98.208:9093 --producer_credentials producer:producerpass --consumer_credentials consumer:consumerpass --kafka_truststore_path ./ca.crt --storage_topic test-async-dkg-1
```

Here:
1) `--username john_doe` — pass your username there
2) `--storage_dbdsn 51.158.98.208:9093` — it's the address of Kafka node we're be using for testnet launch
3) `--kafka_truststore_path ./ca.crt` specifies self-signed certificate we're be using for testnet launch

Let this process run.

## Select public keys and submit them to ceremony data repo

On the client machine run the command `get_pubkey`:
```
./dc4bc_cli get_pubkey --listen_addr localhost:8080
```

On the airgapped machine start the console with
```
./dc4bc_airgapped --db_path /tmp/dc4bc_john_doe_airgapped_state --password_expiration 10m
```
And run the command `show_dkg_pubkey`:
```
>>>> show_dkg_pubkey
```

Grab the output of both commands and submit it along with the username as a pull request to the file [dc4bc-conference-call-25-11-2020.json](https://github.com/lidofinance/dc4bc-conference-call/blob/master/dc4bc-conference-call/dc4bc-conference-call-25-11-2020.json) in the ceremony repo https://github.com/lidofinance/dc4bc-conference-call. Check the format in the repo's `README.md`
