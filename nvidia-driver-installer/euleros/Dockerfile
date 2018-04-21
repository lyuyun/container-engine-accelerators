FROM euleros:2.2.5

RUN rpm --rebuilddb && yum install -y gcc bzip2 perl

COPY fp1.tgz /var/paas/fp1.tgz
COPY entrypoint.sh /entrypoint.sh
RUN tar xzf /var/paas/fp1.tgz -C /var/paas/ \
    && rm /var/paas/fp1.tgz \
    && chmod 700 /entrypoint.sh

CMD /entrypoint.sh
