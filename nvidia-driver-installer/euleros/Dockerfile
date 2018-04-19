FROM FROM euleros:2.2.5

RUN yum install -y gcc kernel-devel bzip2

COPY huaweicloud-fpga/fp1 /var/paas/fp1
COPY entrypoint.sh /entrypoint.sh
RUN chmod 700 /entrypoint.sh

CMD /entrypoint.sh
