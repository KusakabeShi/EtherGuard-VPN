import os
import sys
import signal
import subprocess

er = int( os.environ['EG_FD_RX'])
ew = int( os.environ['EG_FD_TX'])

import threading
import time

bufsize=1500

def signal_handler(sig, frame):
    print('You pressed Ctrl+C!')
    os.close(er)
    os.close(ew)
    sys.exit(0)
    
def read_loop(fd):
    print("Sub: Start read fd:",fd)
    with os.fdopen(fd, 'rb') as fdfile:
        while True:
            text = fdfile.read()
            if len(text) == 0:
                print("EOF!!!!!!!!!!!!!!!!!!!!!!!!")
                break
            print("Sub RECEIVED:",text)
        
def write_loop(fd):
    with os.fdopen(fd, 'wb') as fdfile:
        while True:
            print("Sub: Write fd:",fd)
            text = b'\xff\xff\xff\xff\xff\xff\xaa\xaa\xaa\xaa\xaa\xaa' + b's'*88
            fdfile.write(text)
            fdfile.flush()
            time.sleep(1)
            
tr = threading.Thread(target = read_loop,  args=(er,))
tr.start()

tw = threading.Thread(target = write_loop, args=(ew,))
tw.start()

signal.signal(signal.SIGINT, signal_handler)
signal.pause()

# tr.join()
# tw.join()
# os.close(er)
# os.close(ew)