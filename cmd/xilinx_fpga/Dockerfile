FROM euleros:2.2.5
COPY xilinx_fpga /usr/local/bin/xilinx-fpga-device-plugin
COPY FpgaCmdEntry /usr/local/bin/FpgaCmdEntry
RUN chmod 750 /usr/local/bin/xilinx-fpga-device-plugin /usr/local/bin/FpgaCmdEntry
CMD ["/usr/local/bin/xilinx-fpga-device-plugin", "-logtostderr"]
