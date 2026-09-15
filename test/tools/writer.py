import sys, binascii
open(sys.argv[1], 'wb').write(binascii.unhexlify(sys.argv[2]))
