import os, sys, binascii

mode = sys.argv[1]
path = sys.argv[2]
hex_data = sys.argv[3]

if mode == 'w':
    with open(path, 'wb') as f:
        f.write(binascii.unhexlify(hex_data))
elif mode == 'a':
    with open(path, 'ab') as f:
        f.write(binascii.unhexlify(hex_data))
