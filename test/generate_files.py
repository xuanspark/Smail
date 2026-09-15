import sys
import binascii
with open(sys.argv[1], 'wb') as fp:
    for line in sys.stdin:
        line = line.strip()
        if line:
            fp.write(binascii.unhexlify(line))
print('done')
