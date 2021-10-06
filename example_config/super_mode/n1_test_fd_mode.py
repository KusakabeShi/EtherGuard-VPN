import os
import sys
import signal
import subprocess

pr, ew = os.pipe() 
er, pw = os.pipe()

import threading
import time

bufsize=1500

def signal_handler(sig, frame):
    print('You pressed Ctrl+C!')
    os.close(pr)
    os.close(pw)
    sys.exit(0)

def read_loop(fd):
    print("Main Start read fd:",fd)
    while True:
        text = os.read(fd, 65535)
        if len(text) == 0:
            print("EOF!!!!!!!!!!!!!!!!!!!!!!!!")
            break
        print("Main: RECEIVED:",text)
        
def write_loop(fd):
    while True:
        print("Main Write fd:",fd)
        text = b'\xff\xff\xff\xff\xff\xff\xaa\xaa\xaa\xaa\xaa\xaa' + b'm'*88
        os.write(fd,text)
        time.sleep(1)
            
tr = threading.Thread(target = read_loop,  args=(pr,))
tr.start()

tw = threading.Thread(target = write_loop, args=(pw,))
tw.start()

os.environ["EG_FD_RX"] = str(er)
os.environ["EG_FD_TX"] = str(ew)

print(str(er), str(ew))

#p = subprocess.Popen('./etherguard-go -config example_config/super_mode/n1_fd.yaml -mode edge'.split(" "),pass_fds=[er,ew])
p = subprocess.Popen('python3 example_config/super_mode/n1_test_fd_mode2.py'.split(" "),pass_fds=[er,ew])
#p = subprocess.Popen('example_config/super_mode/n1_test_fd_mode2'.split(" "),pass_fds=[er,ew])

os.close(er)
os.close(ew)

signal.signal(signal.SIGINT, signal_handler)
signal.pause()

# tr.join()
# tw.join()
# os.close(pr)
# os.close(pw)