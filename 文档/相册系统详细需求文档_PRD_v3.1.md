---
title: 相册系统详细需求文档 (PRD v3.1)
---

<Heading id="sDVp52Y8h0e5pXwaQ3e00X" level="1">
  全景相册系统 · 详细需求文档（PRD v3.0 · 100% 自研）
</Heading>

<BlockQuote id="VMAwJ5I3QqOfUiobtMKYyy">
  <Paragraph id="YwZTODWoIAdPrirzfWa1c3">
    版本：v3.1 ｜ 状态：草案 ｜ 日期：2026-08-24\
    模式：<Mark bold>100% 自研</Mark>（自写后端/前端/AI/转码，不 fork 任何产品；底层复用通用开源库）\
    参考策略：<Mark bold>功能点优先对标群晖 Synology Photos 套件（示例环境：用户当前在 DS1819+ DSM 7.2.1 上实际使用的相册；**仅为对标参照，非部署目标**）；Synology Photos 不具备的能力，参考 PhotoPrism 的做法</Mark>\
    用户刚需（须全部覆盖）：4K+ 360° 全景视频、手机/PAD 陀螺仪、VR/AR 头显头追、自适应码率、微信 H5 链接分享（非整文件）、时间轴、GPS 地点分类
  </Paragraph>
</BlockQuote>

<Divider id="Bsho9LHW4MfEzVsmldQAvP" />

<Heading id="H7y1eX89j7AXMXuN1mpwY0" level="2">
  0. 文档信息
</Heading>

<Table id="DqVOQ6lBiVPTIbIXTkSE8I" readonly rowHeader>
  <TableRow id="hvQiqNbSalp19IkZeYdr3U">
    <TableCell id="UNLCUuD0nEddhr1YlGBlrA">
      <Paragraph id="BDbpVHOPb4eeU4OSgzKyRd">
        项
      </Paragraph>
    </TableCell>

    <TableCell id="HwQIhDdY3zh0zLUn4FrJRa">
      <Paragraph id="l7UdEBFILpnr4pFn87aXQf">
        内容
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="45KfxoXQbeIzg67r7PNN1u">
    <TableCell id="BTI0dyHt7CZ9oIHfMtPon0">
      <Paragraph id="rSWMRPe7LJUWNmYY9AF3FK">
        文档类型
      </Paragraph>
    </TableCell>

    <TableCell id="wgJYPOBe0nYzpyzFNzAuFQ">
      <Paragraph id="vHobzONu1BrrCadq0e3tZ3">
        产品需求文档（PRD）+ 技术实现路径
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="kVBiujwYRHaVSd6fZUg4kl">
    <TableCell id="G4aRgp3BCQ0rDxtzM1UWB3">
      <Paragraph id="FSqy2PkLlpHG524TBrN5al">
        目标读者
      </Paragraph>
    </TableCell>

    <TableCell id="hQlilA6kbv1qnncU5IRCfO">
      <Paragraph id="vUZtJ4a19M4hoKnE56dYDi">
        产品 / 架构 / 前端 / 后端 / 算法 / 运维
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="pUGV5fIPkfsE2cqoowCZLO">
    <TableCell id="620rtBDgiDB8iUWjIWTxwq">
      <Paragraph id="pwhDXT98M8jvptuYz26sDS">
        交付粒度
      </Paragraph>
    </TableCell>

    <TableCell id="Y2HsVKsahkXkgeo6R5Cobt">
      <Paragraph id="wHlfF7Rzh6fMjAbccUTxBd">
        功能页面布局 + 单功能点实现路径 + 参考来源
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="TdZC0rzYYDRjPsGHDgTfpI">
    <TableCell id="HT1uGKWR4QhHgtnTEGrLAR">
      <Paragraph id="cRz22U7HnbDLM3hYx3OdIY">
        替代说明
      </Paragraph>
    </TableCell>

    <TableCell id="UwuQLChc9nRcq2S9hjfosZ">
      <Paragraph id="fXkcSVFiSj2RlBDo1XCaTt">
        替代 v2.0（v2.0 误将"Photos"理解为 Apple Photos）。本版以<Mark bold>群晖 Synology Photos</Mark> 为先、PhotoPrism 为补
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="ph3r3dnmxI4VMYbDfVo34R">
    <TableCell id="cV6ZOfAXpCGNAm65WC1nj1">
      <Paragraph id="qqIA4Es9OmzlElk24wBHmH">
        背景约束
      </Paragraph>
    </TableCell>

    <TableCell id="Mt8KQEVE3J3hKVLp3YZTOt">
      <Paragraph id="JjOR8m9Zi0O3SFRaGBZJD2">
        当前环境（**示例环境，非目标平台**）：DS1819+ / DSM 7.2.1 / Synology Photos；大量 H.265/H.264 全景视频经 SMB 持续入库；主力使用手机 App + Web 分享。⚠️ 本系统为通用自托管相册系统，可部署任意主流 Linux / Docker 或独立服务器，**不绑定群晖与任何机型**；本行仅记录开发期使用的一台代表设备。
      </Paragraph>
    </TableCell>
  </TableRow>
</Table>

<Divider id="TxH3YIV5IQHYvh09it1Ubq" />

<Heading id="bVdtUIyLufbSfbKvDFxmrM" level="2">
  1. 产品概述
</Heading>

<Heading id="BudCaXSrapylqD8EG0GVy5" level="3">
  1.1 愿景
</Heading>

<Paragraph id="AvRIPfklCnxtioAC5uBBIA">
  一套<Mark bold>通用、自托管、隐私优先</Mark>的相册系统，<Mark bold>部署不绑定群晖</Mark>（可部署任意主流 Linux / Docker，或独立服务器安装；媒体源位置无关）。功能上以 <Mark bold>Synology Photos 为对标参照</Mark>：先完整复刻群晖 Photos 的消费者/家庭 NAS 体验（时间轴、个人空间/共享空间、智能/普通/共享相册、人物、地点、标签、文件夹视图、收藏、回收站、移动端自动备份、链接分享），再补齐 <Mark bold>其对标之外的 360° 全景视频交互播放、增强 AI 标签、自适应码率、微信 H5 分享</Mark>——这些缺口参考 PhotoPrism 的成熟做法或纯自研。
</Paragraph>

<Heading id="01GCWPYfbqCTKn7N3IBq2z" level="3">
  1.2 范围
</Heading>

<BulletedList id="1P045S1VlgCrTHGMJuHuOZ">
  <Mark bold>IN</Mark>：时间轴/相册/人物/地点/标签/文件夹/收藏/回收站/查看器/基本编辑/360 球面照片/分享/移动端备份/多用户空间；★360 视频交互引擎（陀螺仪/VR/自适应码率/微信 H5）。
</BulletedList>

<BulletedList id="kHvVt0G0syIShSEuHwbXws">
  <Mark bold>OUT</Mark>：即时通讯、协同文档、公有云 SaaS 运营（除非自架托管）。
</BulletedList>

<Heading id="jKIFjHnofQSbLF89djvo6A" level="3">
  1.3 「100% 自研」的含义
</Heading>

<BulletedList id="Lliyrt9pAWAAwEyHQ07zH8">
  自写：API 网关、媒体索引、AI 推理编排、转码管线、分享/鉴权、前端（Web/PWA/360 播放器）。
</BulletedList>

<BulletedList id="18tV68qDxx2Jjt4VuTqxau">
  复用通用开源库（非产品级 fork）：Three.js（渲染）、MapLibre GL（地图）、hls.js（自适应）、ffmpeg（转码）、OpenCV Zoo YuNet+SFace（人脸，Apache-2.0）、PostgreSQL+pgvector（存储/向量）。
</BulletedList>

<BulletedList id="gGc3PjEAsUNOjF0zNJ2OLs">
  <Mark bold>不 fork Synology Photos / PhotoPrism / 不依赖厂商私有框架</Mark>；仅在"设计取舍"上参考二者。
</BulletedList>

<Heading id="D3u9weu7uY0jvRckjVlGcU" level="3">
  1.4 参考来源对照表（关键决策依据）
</Heading>

<Table id="ENO55yzZSK7aPlwRnnnqEr" readonly rowHeader>
  <TableRow id="m1OqTLB9GbPSbK4yvJP0UP">
    <TableCell id="s04ngvw4d6v1HtosoTNtCY">
      <Paragraph id="u57ZGC57g3Mxtry1gKzkAO">
        能力
      </Paragraph>
    </TableCell>

    <TableCell id="m4rYWLq7ZETq8lmEhyBYa6">
      <Paragraph id="4JlTsZSOJc1dwBuhjyP3i3">
        群晖 Synology Photos
      </Paragraph>
    </TableCell>

    <TableCell id="iuMx4hQLrJLeLUxQWjkGIP">
      <Paragraph id="EpEEOfMpqCZH5AsnyEHGZJ">
        PhotoPrism（补位）
      </Paragraph>
    </TableCell>

    <TableCell id="DxuttWxDJSp8njh1TNYClg">
      <Paragraph id="yaV2P7jfl7VArIIBmIKSQz">
        自研（二者皆无）
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="AoogTTE0JhpBynulKub3uh">
    <TableCell id="5Ivt3zLCLfEPHuKMDxeq7p">
      <Paragraph id="fzMPbKgdq9ppRRBA1Hwl6a">
        时间轴 年/月/日
      </Paragraph>
    </TableCell>

    <TableCell id="YBEjqRMQknbiNH5SRWfvtH">
      <Paragraph id="ALdAY2KEoW9oI0AVxmaCML">
        ✅ 原生核心
      </Paragraph>
    </TableCell>

    <TableCell id="hCaHSCYl3vQumTpmGczciI">
      <Paragraph id="EOpwCmzSMK3ymdi4fuWWEc">
        有
      </Paragraph>
    </TableCell>

    <TableCell id="eqlkEhF2ja2YcUHGEbDDDv" />
  </TableRow>

  <TableRow id="P2vWHTiUh0lcO5W4O1erZE">
    <TableCell id="nEEuNX6w2FMkLyBU3NnW4V">
      <Paragraph id="0ysU7nnT3KbK2ezp3P2ebm">
        个人空间 / 共享空间双空间
      </Paragraph>
    </TableCell>

    <TableCell id="kDbdju7D2qSXpdzC80rKZY">
      <Paragraph id="T3YTkZEWD4h6s9sCsAGWU5">
        ✅ 原生
      </Paragraph>
    </TableCell>

    <TableCell id="mqrWajzGdeeY0pAZbqA9eB">
      <Paragraph id="mOq8Pns2a740QWV02yaGZP">
        多用户/RBAC
      </Paragraph>
    </TableCell>

    <TableCell id="U1ZNcMFPBRNjqjtqBLaTVC" />
  </TableRow>

  <TableRow id="y3AJGaWc3UMISwKaZxnLmn">
    <TableCell id="n4o7sJozCGPRb0MpXE1chF">
      <Paragraph id="jWPDvs0ksEPTBrUSzBeCGy">
        相册：普通 / 智能 / 共享 / 收藏
      </Paragraph>
    </TableCell>

    <TableCell id="fIJ5EyGfHl2cQkkS1d1tYA">
      <Paragraph id="zcX7FrXRzHPe8y1izz3luD">
        ✅ 原生
      </Paragraph>
    </TableCell>

    <TableCell id="ZSA94cdbMdCnEaSszZm4iT">
      <Paragraph id="mEfpI46japdtiILS8dshiV">
        ✅
      </Paragraph>
    </TableCell>

    <TableCell id="qLR0XCBIGwThCZZesD1qrA" />
  </TableRow>

  <TableRow id="Xmf5nU1h3ZCb39tTy5X9E2">
    <TableCell id="1hR6AhM9zT3XVngcjYYuRY">
      <Paragraph id="t6wDMyHKpVLSQBTTCDXej5">
        人物识别与命名、合并
      </Paragraph>
    </TableCell>

    <TableCell id="GH9nS4f7e6FtbfOakR68sr">
      <Paragraph id="zFpvlbbKzm8uSoTsa3hcmP">
        ✅ 原生（DSM AI）
      </Paragraph>
    </TableCell>

    <TableCell id="xdIjdmELonpJYCpbXattdh">
      <Paragraph id="BpMajQsR9o0aMleCmfGQgV">
        ✅ 人脸
      </Paragraph>
    </TableCell>

    <TableCell id="MIoNI1NU3rAr4su1dvhR8z" />
  </TableRow>

  <TableRow id="lw4vAsMZhi0tM51H8xSo8J">
    <TableCell id="E2xGu2Fbrdap9ht8l7gf2Z">
      <Paragraph id="U4ZPfWiBvpUo2aIhZVVo70">
        地点地图（GPS 聚合）
      </Paragraph>
    </TableCell>

    <TableCell id="VJfpQ4fOP5p4LiKBQD9i7z">
      <Paragraph id="9WRhgXTMwuOsE2sg2Xk34S">
        ✅ 基础地图
      </Paragraph>
    </TableCell>

    <TableCell id="jUX4SDhRAsfuqKAsQrwhV7">
      <Paragraph id="n2qYSatKedYAhtC2Vwc61I">
        ✅ + 3D/卫星增强
      </Paragraph>
    </TableCell>

    <TableCell id="fWJapbFeLyKSK05BRIynxF" />
  </TableRow>

  <TableRow id="rwNBmt5RFdpHBCPnm8vouc">
    <TableCell id="0k583TJoNZUrOUtydEYuX6">
      <Paragraph id="tRnTiVH0piWBqME3bkukWJ">
        <Mark bold>地图模式（全屏交互）</Mark>
      </Paragraph>
    </TableCell>

    <TableCell id="LVT2hDv6zVFQxF3zeSZcb9">
      <Paragraph id="VeosF0l5qmPnpz5gN7TsSl">
        ❌ 仅 2D 聚合
      </Paragraph>
    </TableCell>

    <TableCell id="LJvqWLZkCUYLp2PegpzrTL">
      <Paragraph id="lIpPdpWfWTlMCjPMmdbr2O">
        ⚠️ 3D/卫星增强
      </Paragraph>
    </TableCell>

    <TableCell id="n04elNuojM3575gOV0upsb">
      <Paragraph id="Fz14yl6bRkGoZvM04QF22t">
        ✅ 本系统新增（世界地图/城市级缩放/时间轴滑块/筛选栏/模糊搜索/中外国界可配·高德中国+国际底图）
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="N5mXbfhjMFJ2MZGpmkqk0z">
    <TableCell id="ITk6cwpdZCjbXjN2yphQM2">
      <Paragraph id="Wurkkw0QCvUNGp5LwGdSj5">
        标签（用户标注）
      </Paragraph>
    </TableCell>

    <TableCell id="byHln20ZjzFSLFqZfWb74f">
      <Paragraph id="3bBODXgHAD1aedYFN0hA2o">
        ✅ 原生
      </Paragraph>
    </TableCell>

    <TableCell id="MJUrKLP3XqsjkmoiAfmTGV">
      <Paragraph id="4c3VUavm6jxs95n1JaNhBI">
        ✅
      </Paragraph>
    </TableCell>

    <TableCell id="zoUAehjE2chmSMdvVFveIi" />
  </TableRow>

  <TableRow id="fg46jZ2e2M17S1HHEa9gfq">
    <TableCell id="qn335bS8DHsJSUqrtbk8wc">
      <Paragraph id="Uy2TcRea56d6rAsDrbhXP0">
        文件夹视图（保留目录结构）
      </Paragraph>
    </TableCell>

    <TableCell id="oZMRZ7z2h4iVRIFqBhgdFL">
      <Paragraph id="shHfGIUsc6aIFEJbV08Kky">
        ✅ 原生标志性能力
      </Paragraph>
    </TableCell>

    <TableCell id="Wf2NQFC3YLS0HwtqANxU7c">
      <Paragraph id="pM8ohFJhUAJviS6tiB8pNa">
        ⚠️ sidecar 思路
      </Paragraph>
    </TableCell>

    <TableCell id="4Sm30AGl5OZRX5QCyYpI8E" />
  </TableRow>

  <TableRow id="aFndmpJIV6PKBcWtfHnMza">
    <TableCell id="d0Uky6LXdIbhCNRI1eK2pz">
      <Paragraph id="yzHeL1YLYD9DDo0ibfXJJf">
        收藏 / 回收站
      </Paragraph>
    </TableCell>

    <TableCell id="XFkOA89SqTToxdAgzMLRad">
      <Paragraph id="jU9t5EKXMHO87SDjefuOGq">
        ✅ 原生
      </Paragraph>
    </TableCell>

    <TableCell id="XCBP0XHdPIn0ZzEpdEny2L">
      <Paragraph id="9EayAoqq96MGqwqohYijeV">
        ✅
      </Paragraph>
    </TableCell>

    <TableCell id="QLD8ayMdlAJIVQl2MpkQaD" />
  </TableRow>

  <TableRow id="rw9f3Qsq1ZTRzQ8lIJYrOg">
    <TableCell id="US4IuDHB3PfTeqghzbfuaD">
      <Paragraph id="40TrWttPLT3iFGCmpiIQMJ">
        查看器 + 幻灯片 + 基本编辑（旋转/裁剪）
      </Paragraph>
    </TableCell>

    <TableCell id="PfMkDB7DolXOXfF71oSKPO">
      <Paragraph id="Dbgx3wJz7vS0fpO1IUVKcL">
        ✅ 原生
      </Paragraph>
    </TableCell>

    <TableCell id="03xjdTdUPhMAColFLz1J3G">
      <Paragraph id="Ik3myQ4vUXxhTNRYFDvWpL">
        基础
      </Paragraph>
    </TableCell>

    <TableCell id="i72XjV2UeZ7qU4aoOzzRYa" />
  </TableRow>

  <TableRow id="DfpKZMHugvMkDbZGpjkhNG">
    <TableCell id="CfqFWNXz8q7jzt0KSm6ypE">
      <Paragraph id="yMwN4wCzvGcdGrXnUdKA0H">
        360° 全景<Mark bold>照片</Mark>球面查看
      </Paragraph>
    </TableCell>

    <TableCell id="ooDy9cRpDYwWNyBoPRHskV">
      <Paragraph id="ZK3QvHkbEOQNEPy33J2Bmg">
        ✅ equirectangular EXIF
      </Paragraph>
    </TableCell>

    <TableCell id="KVMpnHm1CEaVTGZJQYsDG4">
      <Paragraph id="LcXZYyfBL4jVNVazjwQyPq">
        ✅ 16K
      </Paragraph>
    </TableCell>

    <TableCell id="hjLHXYine7y0XsfzBjGpJn" />
  </TableRow>

  <TableRow id="ArJvmx2sMtjxlKbdrJ4Da1">
    <TableCell id="k4BXPXgHa7xXnGc4NzKWBo">
      <Paragraph id="9wQ9PZLOxN5jBvNdWEQekb">
        移动端 App 自动备份
      </Paragraph>
    </TableCell>

    <TableCell id="T18ULNT0IwOg66I6jDfpiU">
      <Paragraph id="LR78tTkAOjqHE4Woa6ukaX">
        ✅ 原生 App
      </Paragraph>
    </TableCell>

    <TableCell id="CPnAxRCPCgRHM9SAJfCucA">
      <Paragraph id="trf8inxaZ2rE8SB4IUwY1A">
        ⚠️ 自写 PWA
      </Paragraph>
    </TableCell>

    <TableCell id="EOsncANVdZl7EEL6Xpi8Rj" />
  </TableRow>

  <TableRow id="LmD0WrgKI8ipyjIWs17AgS">
    <TableCell id="sQMr76RPqc6gcvfwsBMoSv">
      <Paragraph id="2pucFlUiVnuoJ0PYKdztq8">
        分享链接（有效期/密码/下载开关）
      </Paragraph>
    </TableCell>

    <TableCell id="KmtE6f6zhGV9rE3hU7UkdD">
      <Paragraph id="qRuzFMk7F5Yrkmm4QnklC5">
        ✅ 原生
      </Paragraph>
    </TableCell>

    <TableCell id="hgDSxyHAgDbOuR0ARXWSha">
      <Paragraph id="I2RPs4Ade8HTo7PXCyT39i">
        ✅
      </Paragraph>
    </TableCell>

    <TableCell id="M90h1hR38DUAAax6ydzJjA" />
  </TableRow>

  <TableRow id="gVXlPv80O4JFSjbmGEEm77">
    <TableCell id="ymfqgvrOUqurd4CcmKbWiq">
      <Paragraph id="whgUHMaathv2MY0yGSRzvl">
        多用户 / 独立后台账户（不对接 DSM）
      </Paragraph>
    </TableCell>

    <TableCell id="z61WcklCqwH0kGw6IOc6C3">
      <Paragraph id="i0hMkB4oCHGH13WohK8gBo">
        ⚠️ 群晖用 DSM 账户（非独立）
      </Paragraph>
    </TableCell>

    <TableCell id="k8wWItqzcwiSaybqNns8rE">
      <Paragraph id="MIO4xTh7n3XQVvP2oldwH6">
        ✅ RBAC/SSO/2FA
      </Paragraph>
    </TableCell>

    <TableCell id="JgtlOEYgS1txjKoucEsNX1">
      <Paragraph id="pNhcyuhkyfHVzwAq5o5riR">
        本系统采用独立账户体系
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="pI14tIiDVeAifBW7yCRrp0">
    <TableCell id="9HS3PS5wPVARmiSDTWizBR">
      <Paragraph id="drxFhUsYentmlUhYi7IQLr">
        <Mark bold>360° 全景视频交互播放</Mark>
      </Paragraph>
    </TableCell>

    <TableCell id="rNpkk5UiIX1JIrHWKLyeTz">
      <Paragraph id="uCJuvaWBsXRI1YIAG8OH0Q">
        ❌ 仅 360 照片，视频无交互
      </Paragraph>
    </TableCell>

    <TableCell id="q2PSPoAHg2SwJequenGTYa">
      <Paragraph id="fVv7m98pIXcZPwxd3XRKFQ">
        ⚠️ 仅 equirect 球面，无陀螺仪/VR/ABR
      </Paragraph>
    </TableCell>

    <TableCell id="sBp7Okxx9w3iHZNiEWkkFM">
      <Paragraph id="N1A1HeNWG6yBb9Wu70mH11">
        ✅ 必自研
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="7xRusMx7ofdbrCroC60nu8">
    <TableCell id="xwUz2BdCSvn6mwwGYXztMF">
      <Paragraph id="n5PU3eVGemrRtwwTiPhlso">
        <Mark bold>增强 AI 标签（物体/场景/NSFW）</Mark>
      </Paragraph>
    </TableCell>

    <TableCell id="KLv7JPTFyAz2QATcZ6Cz4B">
      <Paragraph id="Qx3dhPn528AL1xXkh2ZYTb">
        ⚠️ 仅基础人物
      </Paragraph>
    </TableCell>

    <TableCell id="xVNNKR1mcrxZXhpvElDuJg">
      <Paragraph id="pFFjks8cjS0aXbvXgiLfNi">
        ✅ 自动打标
      </Paragraph>
    </TableCell>

    <TableCell id="4EBVo69tWJvvLPk4fsXhp3" />
  </TableRow>

  <TableRow id="g6mHQAX5k8EPrgj02GABgc">
    <TableCell id="FOzuA8ZdeJRxoMfyQvKgEw">
      <Paragraph id="QFVaNRR8s4B1cGDcbtoeYY">
        <Mark bold>3D 矢量/卫星地图增强</Mark>
      </Paragraph>
    </TableCell>

    <TableCell id="xiMCD4lsP2AhhknXfc7z7w">
      <Paragraph id="7TXKTnFXDGlG4ZL2f2sGaR">
        ❌ 仅 2D 底图
      </Paragraph>
    </TableCell>

    <TableCell id="Ue4eD0JJRbRQkrJOzNg5cr">
      <Paragraph id="cvhZWoOVVkize2TbHrjoHA">
        ✅ 付费增强
      </Paragraph>
    </TableCell>

    <TableCell id="XqztM1lOo4Dsl6ju41DHzZ" />
  </TableRow>

  <TableRow id="yidxSaA7ZhpPzNv5PDYIMf">
    <TableCell id="AIp5wBMKzFXLBa9dAOwtOk">
      <Paragraph id="3283PKyoKP7O5hnWtPvYzO">
        <Mark bold>自适应码率 HLS 远程串流</Mark>
      </Paragraph>
    </TableCell>

    <TableCell id="vNvQXeORzJbWdvlOzNtFL1">
      <Paragraph id="bSbcwst86mYPYcgFfo4KN3">
        ❌
      </Paragraph>
    </TableCell>

    <TableCell id="TgjbOdDQ0m3ZFuU3xvbkKq">
      <Paragraph id="zu9BRL5daEB0I7ihmRgOrn">
        ⚠️ 基础转码非 ABR
      </Paragraph>
    </TableCell>

    <TableCell id="j7lkIuXf97lA03MmDD8mpk">
      <Paragraph id="e3ykVdHnEcEB34Igh856pI">
        ✅ 自研
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="DdTUsdJJNPP4z5axzGsFW4">
    <TableCell id="koN37LVXPxDMndoNAdMTdv">
      <Paragraph id="drQJbiKNveb6PBprtgOX6D">
        <Mark bold>微信 H5 链接分享（非整文件）</Mark>
      </Paragraph>
    </TableCell>

    <TableCell id="gvKnLI8GkTDtkslbNzCgFR">
      <Paragraph id="E053XnBOL0LwOIp3YZ8gjU">
        ❌ 普通链接非微信优化
      </Paragraph>
    </TableCell>

    <TableCell id="kq4MCeX2q4u5wA01akyUS8">
      <Paragraph id="ErQGhL7AYvktJOzw2uTInd">
        ⚠️ 链接分享非微信
      </Paragraph>
    </TableCell>

    <TableCell id="zB3ja8wMpCQERUE9p2GOkP">
      <Paragraph id="mDjuO5c7OS0VdMH3ThzKir">
        ✅ 自研
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="wYLNvMktptSl5vUsxJBS8c">
    <TableCell id="Id6kmPAn543pEetgDa1KCP">
      <Paragraph id="MHM8TW0VF4B0MOFVce26E1">
        <Mark bold>陀螺仪 / VR 头追</Mark>
      </Paragraph>
    </TableCell>

    <TableCell id="V3IFepD3Z8jtG9hmEgiT5K">
      <Paragraph id="T5dtAxfD7sT2GcsTjxZDhr">
        ❌
      </Paragraph>
    </TableCell>

    <TableCell id="TwQq1Zvl7eMieELjcI6bGk">
      <Paragraph id="uIDXpGLjpJ3zvzwyvSd5Bk">
        ❌
      </Paragraph>
    </TableCell>

    <TableCell id="5NGu1Q5DyP4lZ3z83V2fOQ">
      <Paragraph id="vKSTrixbMv41ZgiztKdrfL">
        ✅ 自研
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="U5UDNrr95JuGU9EWAHGhAC">
    <TableCell id="jFonLvDVw7OkFV6NvBQ2rT">
      <Paragraph id="AXCHnOYe9HegfwNcsCK3Jp">
        <Mark bold>WebDAV 直出媒体</Mark>
      </Paragraph>
    </TableCell>

    <TableCell id="RXL0IjfvDD0bjHlLOGJOqb">
      <Paragraph id="3Fs7x2DgDKpyD1zwockGuf">
        ❌ Photos 不走 WebDAV
      </Paragraph>
    </TableCell>

    <TableCell id="cUuHcuGAXYuKFBxJbN7JmH">
      <Paragraph id="ZPFJeRescVIEELxtH8MAY8">
        ✅
      </Paragraph>
    </TableCell>

    <TableCell id="RUQowRL02vYn8733GSHRqZ" />
  </TableRow>

  <TableRow id="a76XbkaU607pSApb0bJbgb">
    <TableCell id="B8YoPd2TFUIORONzeKEKv9">
      <Paragraph id="QhcOGDwL3r296oPsF4i0yS">
        <Mark bold>自然语言搜索 / 高级筛选</Mark>
      </Paragraph>
    </TableCell>

    <TableCell id="sd8gLFkriu7iNrT0zXsb1E">
      <Paragraph id="SoHzned7Px1HLUoENO45e1">
        ⚠️ 仅基础筛选
      </Paragraph>
    </TableCell>

    <TableCell id="A0aCzLIg7xBeq2A2MmlR1n">
      <Paragraph id="WDRFPvNTPEOjHioswycr7y">
        ✅ 高级筛选
      </Paragraph>
    </TableCell>

    <TableCell id="idC7MjjhQMxXmsgR5SJJoD">
      <Paragraph id="z7027da5Pj1AFWMnpxWQWz">
        自然语言自研
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="Qs0YWCR7Rq9GronoHpKzLR">
    <TableCell id="OkIWUdhiGi5955qrqeZBQp">
      <Paragraph id="Vv6SB7CvnhXIJbRYQ3szW9">
        <Mark bold>去重 / 最近删除恢复 / 工具箱</Mark>
      </Paragraph>
    </TableCell>

    <TableCell id="xcuejqn1LeN2IjgnpnaNeQ">
      <Paragraph id="9zEhMGZuq95sPxiotVTAY8">
        ⚠️ 回收站基础
      </Paragraph>
    </TableCell>

    <TableCell id="4h4tkIx3a98qJNThBLMmWZ">
      <Paragraph id="E7WFdTdGAXJd7qaIp67h4m">
        ✅ 去重
      </Paragraph>
    </TableCell>

    <TableCell id="NolVzZOyBmhEij08Ybt1D7">
      <Paragraph id="b88uLWtTlfOt2o0Y4py4Un">
        去重参考 PP
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="LPKgdOJJ0VYFB2pl2QMle3">
    <TableCell id="cbKF5LTv4TOJJhNrrGsSmT">
      <Paragraph id="vOrEBT6zzn6fpQchgzDyIt">
        <Mark bold>回忆影片 Memories</Mark>
      </Paragraph>
    </TableCell>

    <TableCell id="VIMhT5VhVkjNRJC4QtAYGt">
      <Paragraph id="jRvqN128mR2if2jwcQ5oj8">
        ❌
      </Paragraph>
    </TableCell>

    <TableCell id="Hsl3sMj9jOJk2Wx8Gvrhwx">
      <Paragraph id="Y6aDN8P4WhrSXegpn8cHSF">
        ❌
      </Paragraph>
    </TableCell>

    <TableCell id="sRvUlqqLuaRr2yKqp5HUlc">
      <Paragraph id="X9sJVt6exSVI6meau3B4ee">
        ✅ 自研（可选）
      </Paragraph>
    </TableCell>
  </TableRow>
</Table>

<BlockQuote id="JvIMF1XShhNOx7myLB523s">
  <Paragraph id="YKlqvHUm5NTcrMnLH2xw3g">
    <Mark bold>账户与权限决策（已确认）</Mark>：权限管理、多账户管理<Mark bold>不遵循 DSM 套件逻辑</Mark>，本系统采用<Mark bold>独立后台管理模式</Mark>——自建账户体系与 RBAC，不对接 DSM/目录账户；共享空间等功能的成员与权限均由本系统后台独立配置，与 DSM 解耦。
  </Paragraph>
</BlockQuote>

<BlockQuote id="MUcilIHDP1AbXXURNVIhqL">
  <Paragraph id="y1TEDTeZEIG3SBOmTsM8yi">
    <Mark bold>第二轮开放问题决策（已确认）</Mark>：
  </Paragraph>

  <NumberedList id="L745qFU5vL1KGnRFAMDabd">
    <Mark bold>GPU 算力节点（多形态）</Mark>：支持三类接入且可配置切换——①本地 GPU（本机/同机容器，含 NVENC）；②云 GPU（按需云实例/Serverless）；③本地网络第三方 GPU 主机（局域网内 Windows/Linux，经 agent 接入）。引入「算力节点（Compute Node）」抽象 + 调度器（注册/心跳/能力声明/任务队列）。
  </NumberedList>

  <NumberedList id="BzNdEuVcVTP7QCb3iMFGpF">
    <Mark bold>微信分享</Mark>：H5 链接为优先实现；小程序级体验为第二阶段目标（两者最终都要）。
  </NumberedList>

  <NumberedList id="Zljh3pX7tRjERf2JFVXDq2">
    <Mark bold>共享形态</Mark>：以「共享相册」为主，不要求共享空间「多人实时互见」；共享为异步邀请+角色权限，非实时协同。
  </NumberedList>

  <NumberedList id="89gH2IiHD1qkeBgdscSfgc">
    <Mark bold>网络与带宽</Mark>：支持自定义自用域名 + IP 直接访问 + HTTPS（含非标端口、路由器端口转发映射）；带宽需支持<Mark bold>手动指定上下行</Mark>与<Mark bold>自测带宽</Mark>，并据此<Mark bold>推荐实时码流率</Mark>（ABR 档位自适应）。
  </NumberedList>
</BlockQuote>

<BlockQuote id="BtaqQGSb8ATNXwXOvn4djZ">
  <Paragraph id="OTzwlqjcVIOV1hgMhMRNpg">
    <Mark bold>地图模式决策（新增大功能点，已确认）</Mark>：
  </Paragraph>

  <BulletedList id="ggTu2ccmMxtXXKQh5KMWTQ">
    <Mark bold>全屏地图模式</Mark>为独立视图：默认世界地图，可缩放至城市级（优先高亮库内照片/视频最多的城市级区域）。
  </BulletedList>

  <BulletedList id="wdqLNuKPrtXNpXvRaeHQqE">
    <Mark bold>时间轴滑块</Mark>：位于地图上方或下方（UI 可配置），显示当前筛选 + 当前可视区域内媒体的时间跨度；拖动/点击可定位到年/月/日或任意时间段（默认不限），即时刷新地图缩略图。
  </BulletedList>

  <BulletedList id="yS4H5YxBHLVyNXAk1REhAs">
    <Mark bold>筛选栏</Mark>：位于地图左或右侧（UI 可配置），按 类型/时间/GPS 标签/人物/标签 组合筛选，支持模糊匹配。
  </BulletedList>

  <BulletedList id="r6CsnW9b5oGgddS03oUEj5">
    <Mark bold>地理模糊搜索定位</Mark>：同时支持中国（建议<Mark bold>高德 Amap</Mark> API，密钥手工设置）与国外（OSM/MapLibre 等）地图资源，可一键切换；照片 GPS 多为 WGS-84，叠加高德底图需做 <Mark bold>WGS-84 ↔ GCJ-02</Mark> 坐标转换。
  </BulletedList>
</BlockQuote>

<Divider id="zMPGypxRDVJu2v0uw2VOVZ" />

<Heading id="sziEqc06tAfVRGDkcYahse" level="2">
  2. 用户角色与用例
</Heading>

<Table id="DAUFyrvjFEFVJOmjl5tTdz" readonly rowHeader>
  <TableRow id="DA14QUtU0gryDr5kjYhUpv">
    <TableCell id="MbGHaruV7n7KhDwja7jRKw">
      <Paragraph id="tgj6XrCjLfghBZ07Wb86So">
        角色
      </Paragraph>
    </TableCell>

    <TableCell id="YIiHTTh7iWRLBSadcfnODt">
      <Paragraph id="e0QIZZSQhbL6Px1hdKgQAW">
        说明
      </Paragraph>
    </TableCell>

    <TableCell id="rMBH4kFpfhEwZ7zbEjrgS2">
      <Paragraph id="d5DzVOjpD3mouk72Ggj58Q">
        关键用例
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="hQ7QcS2rRX4tk3z5xUVg8P">
    <TableCell id="vaxSDmaZ0c3yQVk0OM33mb">
      <Paragraph id="PYlOTlhcKEqriVwGvUZCnv">
        图库拥有者（Owner）
      </Paragraph>
    </TableCell>

    <TableCell id="2oRXDls7HWKPlDM4QX0zLX">
      <Paragraph id="cz0RzBc8xuH5una3uSE41c">
        全部权限
      </Paragraph>
    </TableCell>

    <TableCell id="rqOqoQOTVsohOCjmfkHAVi">
      <Paragraph id="lHe0QZeLyLnuFRR2pKDXWA">
        导入、浏览、AI 整理、360 播放、对外分享
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="7yZmpdqjGkj4UL3R3BymUT">
    <TableCell id="dKx5RH2CEcuAjrr3vawLHb">
      <Paragraph id="x9u7USHJ33s9g7OMqwiDGO">
        家庭成员 / 协作者
      </Paragraph>
    </TableCell>

    <TableCell id="Ne6vwgl7xDQ0uu6yMn55fH">
      <Paragraph id="gQKPQX5uKans2tcotFc3hY">
        受限查看/上传（共享空间）
      </Paragraph>
    </TableCell>

    <TableCell id="sikfNggU8OhsUIIbIb0ZWS">
      <Paragraph id="gm9ca1mv4tZZs5akEMyEmc">
        按相册/共享空间授权，微信链接观看
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="JWAecDVu0PZC5JYo7Oxe4S">
    <TableCell id="RJtXGbAt0tpfPSzAbe3onk">
      <Paragraph id="arag5ia3E3WQnoQUOk8YdF">
        系统管理员（Admin）
      </Paragraph>
    </TableCell>

    <TableCell id="9Qk3LvkdJNd7h9YvYr17rt">
      <Paragraph id="CJPgBr9Yn6f5yDdWqZWP6X">
        独立后台账户与权限
      </Paragraph>
    </TableCell>

    <TableCell id="doxQepo4SQ4Zc6A4a5OnFo">
      <Paragraph id="LtODvvW3dzGFxHhX9vKcJu">
        用户/角色/共享空间/索引调度/转码队列/审计
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="WXPzOfoKhl0F5P1IQohneV">
    <TableCell id="KgN4DVBlBXAdVhiZYz8C1P">
      <Paragraph id="P2AzDzpH271ke4rySoFJPZ">
        访客（Guest）
      </Paragraph>
    </TableCell>

    <TableCell id="mVRkylnLqG4j0h86jfpU8S">
      <Paragraph id="P4NMrhegHl28FUXT5rIonr">
        仅分享链接
      </Paragraph>
    </TableCell>

    <TableCell id="HJ4gbKkeYFrAqkkjevWYBY">
      <Paragraph id="m1TPrihGiSI3WK26fpP8pi">
        H5 免登录观看
      </Paragraph>
    </TableCell>
  </TableRow>
</Table>

<Paragraph id="JGRIiSwWs79KZYR2rEECLH">
  用例：导入（SMB/WebDAV/App→监听→索引→缩略图+元数据→AI 入向量库）；360 观看（分享链接→H5→HLS 自适应→WebXR 球面→陀螺仪/VR）；共享相册（家庭/协作成员按角色异步共享，对应群晖"共享空间"形态）。
</Paragraph>

<Divider id="imkJeSNbxcLCew0xtg7p9b" />

<Heading id="OXtTPeXeCLtfaTt6obqdum" level="2">
  3. 非功能性需求（NFR）
</Heading>

<Table id="eGOFojH5z6UywbF4BJXxpG" readonly rowHeader>
  <TableRow id="y9CtcKItSjJ6PCKFz1dytj">
    <TableCell id="WSRmlTZqgRBBRWTMdCpZWN">
      <Paragraph id="6hR2XoRyGKaGu5B13Ym4eC">
        类别
      </Paragraph>
    </TableCell>

    <TableCell id="fDRWPzkm8L0BLRk0fT53Ga">
      <Paragraph id="h9oJ6jFuwHBwQnJyDS219M">
        要求
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="Y39wPxOquj8PRpg8gwvfyA">
    <TableCell id="k1KY8IhMC7L9VFZn584mUG">
      <Paragraph id="tCZ2KQHilY5J7ai4X0g2Vc">
        性能
      </Paragraph>
    </TableCell>

    <TableCell id="vxVAEPbG2OX2YB0duMzMNz">
      <Paragraph id="HtfcwCkvJzGZVI2gDUHnzj">
        10 万媒体搜索 \< 1s；缩略图首屏 \< 2s；4K 360 起播 \< 3s（CDN+HLS）
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="uvhulQk6jQoRM4sEdezr6z">
    <TableCell id="7lxhyRvfORITiH79bPXSiH">
      <Paragraph id="ZE5OAq0nROFEZOOUOwh4jc">
        兼容
      </Paragraph>
    </TableCell>

    <TableCell id="XIDj6XvByTRtFnJXph7u1x">
      <Paragraph id="Cu0htfQrpGRMEhDtjjEueQ">
        Chrome/Edge/Safari/iOS/Android；微信内置浏览器；VR 头显（Quest/Pico/Cardboard）
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="RnrK2DT2xc30IRl8nZSLJn">
    <TableCell id="34IgrzEhDyO6bp3wjL1nDy">
      <Paragraph id="svXEvJOiIUBEfyUbzs1IPZ">
        安全
      </Paragraph>
    </TableCell>

    <TableCell id="XK8sVsDuaqkvih9iz4pSIN">
      <Paragraph id="U5Oro4rlYiHFbDzaCLYwpP">
        HTTPS 全链路；独立后台账户/SSO(OIDC)；2FA；API 限额；会话监控；审计日志
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="Xym1FotvdleJdKrG7NPt5f">
    <TableCell id="2kganjpKdTeeHClIozpncR">
      <Paragraph id="sgenXlkeYM4Rrnt1flRgKK">
        隐私
      </Paragraph>
    </TableCell>

    <TableCell id="dJ4524h0dAn3V2GCmuW4S0">
      <Paragraph id="U0dDLIsrC5XY0daEt4KEDv">
        数据不出本地；AI 推理可纯本地（自建模型）
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="dvQ7furF2z5ATgMh9bjch8">
    <TableCell id="MxQ90qLH6S5msz9RkuucQ4">
      <Paragraph id="r77N9UuCyI3dOzii49QB69">
        可伸缩
      </Paragraph>
    </TableCell>

    <TableCell id="pkrzs0Bi2xXAhdGcAaIOhE">
      <Paragraph id="Py2e1Yygqa9Sgj0a7aDOAt">
        存储与算力分离；转码/AI 横向扩展；对象存储（S3 兼容）
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="s7LsC2RMY27P0KYqjRwXfw">
    <TableCell id="Ee3khegrInidSRa7MK7Ebm">
      <Paragraph id="bmHkYSwCOWjIVX1s1ukJ3G">
        可维护
      </Paragraph>
    </TableCell>

    <TableCell id="YZaB0wrSoxl56xa8Y4CdYH">
      <Paragraph id="XSpcbKYH479Iy6soJqHvVt">
        Docker 化；配置即代码；监控（Prometheus + Grafana）；结构化 JSON 日志 + Loki；审计日志永久；CI/CD 流水线 + 灰度部署；健康检查端点（/health /ready）
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="3OQ94f9mgliTEou03KFUzT">
    <TableCell id="4EVuvBTfs2F2ddbV3NYqsY">
      <Paragraph id="DPQASGgF59m3UZtJLe52TJ">
        可观测
      </Paragraph>
    </TableCell>

    <TableCell id="kVQ8pgDI5kRdOVwp5GALJJ">
      <Paragraph id="DilI1kNO0wZLRAwujp0mvL">
        指标（请求延迟/索引进度/转码队列/GPU 节点在线/带宽）；链路追踪（request\_id 贯穿）；告警（节点离线/队列积压/磁盘/证书过期）；备份恢复演练（季度）
      </Paragraph>
    </TableCell>
  </TableRow>
</Table>

<Divider id="iQtSFCGLjDnytwonM1ELR6" />

<Heading id="CeFTdIvFxc1JEKHOVZMWLk" level="2">
  4. 总体技术架构（摘要）
</Heading>

<Paragraph id="eieVTXhAJVPv0q5LKOwWHg">
  四层：客户端（Web/PWA、App、VR 头显、微信 H5）→ 边缘（反代+CDN，HTTPS/ABR/WAF）→ 应用（API 网关·鉴权·SSO / 媒体库·时间轴·相册·人物·分享 / 360 播放 WebXR）→ AI（人脸·宠物·聚类 / 标签·物体·NSFW / 地图·地理编码）→ 转码管线（缩略图 / HLS ffmpeg+GPU / 索引·去重）→ 存储（对象存储·NAS / PostgreSQL+pgvector / 元数据 sidecar）。
</Paragraph>

<Paragraph id="zPSx3udVrSlkpREbK8gAIM">
  <Mark bold>技术栈</Mark>：后端 Go（`go.mod` 声明 1.26）；AI 推理 = Go + CGO + ONNX Runtime（Chinese-CLIP / OpenAI CLIP / YuNet + SFace），**无 Python/FastAPI 服务**；PG 16 + PostGIS 3 + pgvector；前端 Vue3 + Vite + Pinia；MapLibre GL（地图）；A-Frame/Three.js（360/VR）；hls.js（自适应）；ffmpeg 6（subprocess 调用）+ NVENC（可选，无卡回退 CPU）；**自研队列**（语义对齐 BullMQ）基于 Valkey；部署 Docker Compose（基线）/ 独立服务器（二进制 + systemd）。
</Paragraph>

<BlockQuote id="UMq7dVOyPYcGYoeOkHEgUv">
  <Paragraph id="6Y8RYACC3vfz0BO7AWxhjS">
    算力分离（关键，能力模型）：<Mark bold>存储节点</Mark>（任意 NAS / 服务器，只要有磁盘）<Mark bold>只做文件系统/对象存储</Mark>；AI 推理与 HLS 转码放到<Mark bold>独立算力节点</Mark>（带 NVENC 的 mini PC / 服务器 / 云 GPU；CPU 节点亦可）。低算力主机不承担人脸索引与 4K+ 全景转码，否则会卡死。
  </Paragraph>
</BlockQuote>

<Divider id="lkzHbxTBXJjmxvSV6NoUQT" />

<Heading id="6xQwES9QwRNwAsMjA4dQwN" level="2">
  5. 功能模块总览（分阶段，群晖 Photos 优先 MVP）
</Heading>

<Table id="73fsFWIm1n69a2QXrCceeI" readonly rowHeader>
  <TableRow id="Q9asf2eqKfJbHCa7Rc2GZ0">
    <TableCell id="5kYnXZY9zwZBDENvFI3Ge2">
      <Paragraph id="WojFToRjUFEEW0r06Qiagv">
        阶段
      </Paragraph>
    </TableCell>

    <TableCell id="j9Z2hNDn9AV3cQxRYCGWAW">
      <Paragraph id="x94aqxR3sDDOPvs42Xd2Ai">
        模块
      </Paragraph>
    </TableCell>

    <TableCell id="FjASuQhNDxEXlmVLoOfMJE">
      <Paragraph id="Huh9HfybyCloxGMZu6xmTS">
        参考
      </Paragraph>
    </TableCell>

    <TableCell id="81BSpuypCcnTXbowGDyvha">
      <Paragraph id="kSqRyiElLV5YP7Kbr00DEM">
        验收
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="yhqWHuwsePVlpsgrCKHQ7M">
    <TableCell id="Q9GKYXj0LrUnXBsAo3mNgb">
      <Paragraph id="LHVInnTobVFw00UqDy0bzh">
        P0
      </Paragraph>
    </TableCell>

    <TableCell id="04sMyHz48Cwna3wIwcVB5p">
      <Paragraph id="VynB3zOphHiRf57ruFnHRI">
        时间轴 年/月/日 / 缩略图 / 索引
      </Paragraph>
    </TableCell>

    <TableCell id="67G9pERNz3vtgT4H1smNBg">
      <Paragraph id="OrsOWeE3Gqut59swlGcqFw">
        群晖 Photos
      </Paragraph>
    </TableCell>

    <TableCell id="HJVa5vT3k7XnNjOX4NQ8gT">
      <Paragraph id="t1W0WfyC09hzBssZCtbJJF">
        可导入浏览
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="5097GiBtSV9mk6sc4DaV77">
    <TableCell id="TZOAUT0QFmZjqWAo25it3R">
      <Paragraph id="sf6ahrIGrk6YNFaThdFnvT">
        P0
      </Paragraph>
    </TableCell>

    <TableCell id="fPta8VeQuq0x8Tz4ve6fSm">
      <Paragraph id="bKIQFEfC3KGs7G6cEChgNs">
        个人空间 / 共享空间双空间
      </Paragraph>
    </TableCell>

    <TableCell id="wSGfddG6acUgaJUDYBPt9x">
      <Paragraph id="xv00tMYXNIWqqGLJ9yDjbW">
        群晖 Photos
      </Paragraph>
    </TableCell>

    <TableCell id="bKHlRERIQM1TTZbhImk3IA">
      <Paragraph id="Yv6yDIcW6n9r7TKFDaV6sL">
        双空间隔离
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="wmbr9ypUR9uAPaVTCYyIRq">
    <TableCell id="ABQkl1MK9wXZU4ZAOFs5Ni">
      <Paragraph id="TVnMrQqhnA8k6IFdLabqzo">
        P0
      </Paragraph>
    </TableCell>

    <TableCell id="UvweYTCVSuJTpHZ6SrcnWH">
      <Paragraph id="1sPRhtQXWvBZpfA6aOz71q">
        文件夹视图（保留目录结构）
      </Paragraph>
    </TableCell>

    <TableCell id="9htRgZfH52yHmB65lH3JIZ">
      <Paragraph id="8A0Uaavk49pV1F2L4hXSx2">
        群晖 Photos
      </Paragraph>
    </TableCell>

    <TableCell id="FfE4CCxJQdTQB497GFsWFI">
      <Paragraph id="1esJP3abc1LMMBvRbeYAzs">
        原目录可浏览
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="iqCRSVW5R12PRByBkQHDdt">
    <TableCell id="Sgkf6XagIZfykfpYrpTmBN">
      <Paragraph id="yIjhEFL1Ups1FrOiI91Ut8">
        P0
      </Paragraph>
    </TableCell>

    <TableCell id="F11fOB2njj7qUuDwJzx0m1">
      <Paragraph id="XVK8W1n02CRaWOxFvtPJHx">
        ★ 360 播放引擎（WebXR+陀螺仪+VR+ABR）
      </Paragraph>
    </TableCell>

    <TableCell id="Svaq79PkP3xxXBmxBZNKaK">
      <Paragraph id="dDRZml4ewZbUJELDGya10f">
        群晖无→自研
      </Paragraph>
    </TableCell>

    <TableCell id="LIqfsPlBmlEgFEMwojocbs">
      <Paragraph id="ToGYKwnCPfFcuyerOfCaR5">
        全端可交互
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="Fg954TRyTCfWSedYJFu27J">
    <TableCell id="qeJ3UY03Iwo88kpLnS7xk0">
      <Paragraph id="S0z6PC7A4b9cySMcpZzzj6">
        P1
      </Paragraph>
    </TableCell>

    <TableCell id="WXIJe8mRkfGpm3cujOEzVP">
      <Paragraph id="bbI4ARAtdHPsO5gfyzJDtq">
        相册：普通/智能/共享/收藏
      </Paragraph>
    </TableCell>

    <TableCell id="mAOh6PgTiVVy56iVq01QvZ">
      <Paragraph id="aoDMPJ61Zw2Seb94NfBUiP">
        群晖 Photos
      </Paragraph>
    </TableCell>

    <TableCell id="SlntJrYUPIzjGZAiefk3Ds">
      <Paragraph id="V0kQmMu5V2XdiULikJoP6n">
        自动聚合
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="kX7zia5WalctbcoUBavnZb">
    <TableCell id="UL2Kzv05zHnObf3yMW37ic">
      <Paragraph id="lWf37lG0rlPytzUZV5gYgP">
        P1
      </Paragraph>
    </TableCell>

    <TableCell id="ewgnHGpl6l3xyBKc0QtcMA">
      <Paragraph id="5QfUKy2moJiEaVrRgIhUzK">
        人物识别与命名
      </Paragraph>
    </TableCell>

    <TableCell id="nmHhLtolhl8rfUQVTytBRx">
      <Paragraph id="NLuOu5n36hFevX3BCDkoeh">
        群晖 Photos
      </Paragraph>
    </TableCell>

    <TableCell id="SKOIbm4vfY17VwCPFY3LL5">
      <Paragraph id="ijzE3XKdaJScyH9aAJPdE8">
        设备端式聚类
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="fmZReW5zefpvBXrLHHFpca">
    <TableCell id="J8ErZR2tSrP0DShcAhhk9y">
      <Paragraph id="hdNly9Ezqjsxp9gsvqQUYT">
        P1
      </Paragraph>
    </TableCell>

    <TableCell id="VrRje10dfhA4GmhoHZ0H1q">
      <Paragraph id="nUTWSPaSw7XtqSAKuPUMyF">
        地点地图 / GPS 分类
      </Paragraph>
    </TableCell>

    <TableCell id="9e659CKjuvsr8uHYAdwqNw">
      <Paragraph id="yb2MmL4WXLHYrYSng4yvFV">
        群晖 Photos→增强 PP
      </Paragraph>
    </TableCell>

    <TableCell id="u8tvpsi3Ag5oceaxutn5Cr">
      <Paragraph id="UNdh83Z4k2bNVgZxi3mcdq">
        自动聚合
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="SeRYgy5ki9dNTGUIvn1PxT">
    <TableCell id="MvlqfybfHywkKyRgpw3EnW">
      <Paragraph id="esec16oUaAWEvIDeFd8578">
        P1
      </Paragraph>
    </TableCell>

    <TableCell id="Uu2c1FN603YPTd8EXyzhOg">
      <Paragraph id="pnnylFdLjRRQrDbTpi67wE">
        标签（用户+AI 自动）
      </Paragraph>
    </TableCell>

    <TableCell id="yAotpko5nyQUx3YTIXg686">
      <Paragraph id="oGAx2CDqSgOVBFLfCAnc41">
        群晖→PP AI 标
      </Paragraph>
    </TableCell>

    <TableCell id="R6nvCv4ZzMatLfV85scXUu">
      <Paragraph id="JItRKZSt077khKWI2VdEby">
        自动打标
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="8zjJDmhmhh2ZqbxAY2uRpb">
    <TableCell id="3KVc70SLV5hg2uXoEGz9Us">
      <Paragraph id="NpGIIFNn8gJ81payuVa0Ss">
        P1
      </Paragraph>
    </TableCell>

    <TableCell id="cDWatNAfHVnPgrSbxePXB8">
      <Paragraph id="JmcuFprTthkjCE0BGyreSO">
        查看器 + 幻灯片 + 基本编辑
      </Paragraph>
    </TableCell>

    <TableCell id="nPxoWANzSvjbafnMDA6r4A">
      <Paragraph id="6dHcqVrRgjNlw3Nfv9EBS4">
        群晖 Photos
      </Paragraph>
    </TableCell>

    <TableCell id="CTKjdL668nfwHqEqgqwuK8">
      <Paragraph id="Ccm710cikRt8YuncfqFfWH">
        非破坏编辑
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="awkpsquiWte9yrNcq4Bsrs">
    <TableCell id="gdF4PDhiLnv5Xus1VL5wd2">
      <Paragraph id="L48mzBtFnUiLmQ0vqbUyX0">
        P1
      </Paragraph>
    </TableCell>

    <TableCell id="HOoBDqMiHnwc1CpcA6Ly9s">
      <Paragraph id="nyYnk640t3Dc0MVXAPHgFj">
        HLS 转码增强
      </Paragraph>
    </TableCell>

    <TableCell id="vLBBp2fPKBOxLcHjNThhqw">
      <Paragraph id="WftABP4N3cRMrog90Ze7Er">
        PP 增强转码
      </Paragraph>
    </TableCell>

    <TableCell id="Og440gwJV2XdUx3IeAfiZt">
      <Paragraph id="QpzGXEBud0j8OPCKMi0UxC">
        多码率自适应
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="SkbVpVqlNyhtGyBMVTDMKU">
    <TableCell id="pTtr1qu4Xn2qnQ7GRddYd1">
      <Paragraph id="AnZE05zxdBClJHjqkFLRqQ">
        P1
      </Paragraph>
    </TableCell>

    <TableCell id="sZWJ3ZJAfRLcBvjpKUmn37">
      <Paragraph id="Jt0c88u7etaqSviV1tMJnE">
        搜索（筛选→自然语言）
      </Paragraph>
    </TableCell>

    <TableCell id="OmKrBzGJi70RQV55TTRWpI">
      <Paragraph id="xRw2YMuX5NcHRp1IwAvwpt">
        群晖→PP/自研
      </Paragraph>
    </TableCell>

    <TableCell id="drVK2bPSzKBH3sh0XNgkUP">
      <Paragraph id="rkSS2zRln7qbC8Zn8OmUxZ">
        联合检索
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="oqI8VmWlk0VcxKzp9syToK">
    <TableCell id="nR9QaC9OXb3qNkmF2Mb7aZ">
      <Paragraph id="tj4YGfBV1sfedeRmLnQDo7">
        P1
      </Paragraph>
    </TableCell>

    <TableCell id="0uYHdnzGA4Ns6sdRhzOTmE">
      <Paragraph id="id2KGu3DTqnYF6uHnz3lI1">
        微信 H5 分享
      </Paragraph>
    </TableCell>

    <TableCell id="6PUAcih86dTa0mPcpusNao">
      <Paragraph id="L50i7B2fduQniaEM6w5aa2">
        PP 链接→自研 H5
      </Paragraph>
    </TableCell>

    <TableCell id="KiEX1P3UwLqseZBQbABZU2">
      <Paragraph id="CfDeJ6QgFQ84Be0gUKknHE">
        微信内观看
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="a6bv6wRwgVhtZ8ROGeKOzi">
    <TableCell id="ad1qpMXfdHMFWE1Fg01vLz">
      <Paragraph id="asfIjqvz0aIqpXNnfRTb40">
        P1
      </Paragraph>
    </TableCell>

    <TableCell id="MR5e1TbFuHCxT9EDGDuhVr">
      <Paragraph id="qr7eokkp6j5pD9PSLf5Eyb">
        移动端 App / 自动备份 / PWA
      </Paragraph>
    </TableCell>

    <TableCell id="IGfNmlZo5UCR1dd5gZvmV5">
      <Paragraph id="27jPuvkZSYOM5ShmB6IK13">
        群晖 Photos App
      </Paragraph>
    </TableCell>

    <TableCell id="PyNaMYmmvVGtwDj12htKjq">
      <Paragraph id="qaUv0YzS6pgj5L8TJNXt4O">
        自动备份
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="p0YwPETgbS9yYkPKV12UIu">
    <TableCell id="XXRQa5ITDW0HW4Iq8XuxBQ">
      <Paragraph id="8idFL2vbKxz7ZPiccrqNYt">
        P1
      </Paragraph>
    </TableCell>

    <TableCell id="Drhoc9U3ZzrrPkscLEi957">
      <Paragraph id="prXdb0Dr7w6DugeTCnHQCn">
        360° 全景照片球面查看
      </Paragraph>
    </TableCell>

    <TableCell id="RHzvz93JmzRIB0xHJTa0ib">
      <Paragraph id="uZzeFXvycQ0Ofev8YWSdYL">
        群晖 Photos
      </Paragraph>
    </TableCell>

    <TableCell id="nuQNP8vm1gpgZNx46mRDir">
      <Paragraph id="Jabpv6dxhoMb6nTQT0Lqqy">
        equirect 可看
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="quu65VF1DtxmueaQwLFxJS">
    <TableCell id="P2EzXOAY8ArhDhVfD1qRu1">
      <Paragraph id="RS28iKhvcJBxIG27neDHNL">
        P2
      </Paragraph>
    </TableCell>

    <TableCell id="fUnCwyXUiMFKkK4Ni4mlNA">
      <Paragraph id="337dWUhSKhqx8b6aYw5sjo">
        多用户 / SSO / 2FA / 审计
      </Paragraph>
    </TableCell>

    <TableCell id="MIbkoxNhCFSdcFXhj0J2ER">
      <Paragraph id="jeTlXaATzXyqCyK1hktqKh">
        独立后台（参考 PP RBAC）
      </Paragraph>
    </TableCell>

    <TableCell id="ZAR5RvrbY6Rh1xuu7TG2N1">
      <Paragraph id="JUorfOjAhkKUOxJrMLCtd2">
        企业级权限
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="71x6WNw288nCevkT1SRQmq">
    <TableCell id="PCw0uLGE3J4S3jgc1dbhE5">
      <Paragraph id="0wWjAYrBmUtEETgnkpZkZb">
        P2
      </Paragraph>
    </TableCell>

    <TableCell id="0whQAfqVntqrgYzCCwDxmI">
      <Paragraph id="5PwynYC4TKDLAyFTXq1lsl">
        3D 地图矢量/卫星增强
      </Paragraph>
    </TableCell>

    <TableCell id="mz80hK7Hnr0ONSYM0mIf4X">
      <Paragraph id="Pr44LJZz8tOXxccHdV2Qlj">
        PP 付费地图
      </Paragraph>
    </TableCell>

    <TableCell id="oUhD3yozHHD6BpKLeCuHpl">
      <Paragraph id="S9vNLLPPaL2t8XNTV7pKrq">
        建筑挤出
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="Tl4tNTYTU5yuXP62V9bxdT">
    <TableCell id="JUm5RAkiB11oaJKm4bNVaH">
      <Paragraph id="J2dogNodmaeP7Rd8YBfnXR">
        P2
      </Paragraph>
    </TableCell>

    <TableCell id="pkWtGiEMIlobyHzFTrq0uQ">
      <Paragraph id="LGBMK7qOPCA45UVJ3mmLGq">
        工具箱（去重/恢复）/ 回忆影片
      </Paragraph>
    </TableCell>

    <TableCell id="xb6yG9oyTxwqtz7Ika7AP7">
      <Paragraph id="cNdBWzIrtvK2XKAiH7QdvB">
        PP → 自研
      </Paragraph>
    </TableCell>

    <TableCell id="l1WeGwaCKgpMSgQVS3XvqH">
      <Paragraph id="oeIoRBWhQcdCsy84MXXEoz">
        去重准确
      </Paragraph>
    </TableCell>
  </TableRow>
</Table>

<Divider id="KERZu3FoXtEgJGhp89dDz2" />

<Heading id="ovmYilM2VgKQF9znDQET4t" level="2">
  6. 前端页面详细设计
</Heading>

<BlockQuote id="1ePfE3jtVKS6QyjRXbMJgK">
  <Paragraph id="x7bIbBpTrn0IOVAC0oSwTU">
    每页含：布局线框（ASCII）｜功能点｜实现路径｜<Mark bold>参考来源标注</Mark>。
  </Paragraph>
</BlockQuote>

<Heading id="T6CvZhTRhvGQQpjKpXP8lU" level="3">
  6.1 登录与鉴权页  【群晖 Photos 用 DSM 账户 → 细权限参考 PhotoPrism】
</Heading>

<Code id="O8qUNAKnxxiKTM2J9pRfJB">
  ```
  +--------------------------------------------------+
  |                  [Logo]                          |
  |   账号 ____________   密码 ____________          |
  |   [ ] 记住我   [登录]                            |
  |   或使用: [SSO/OIDC] [微信扫码]                  |
  |   2FA: 验证码 ____________  [验证]               |
  +--------------------------------------------------+
  ```
</Code>

<BulletedList id="PKp0SKrNZdaxVT7dTWWsgn">
  <Mark bold>功能点</Mark>：账号密码（<Mark bold>独立后台账户，不对接 DSM</Mark>）、SSO（OIDC）、2FA（TOTP）、"记住我"、应用密码（API）。
</BulletedList>

<BulletedList id="j0sx1psPAoF2WwnpSIHZEu">
  <Mark bold>实现路径</Mark>：鉴权服务签发 JWT；SSO 接 Keycloak/Authelia；2FA 用 `otplib`；失败限流（Redis）；会话入 PG 可监控/吊销。
</BulletedList>

<BulletedList id="bI16b1HqQktFcnc8AWdG2s">
  <Mark bold>参考</Mark>：群晖 Photos 复用 DSM 登录（无独立账号页）；<Mark bold>本系统改为独立后台账户体系</Mark>，细粒度 RBAC/SSO/2FA 参考 PhotoPrism 多用户模型，账户与 DSM 解耦。
</BulletedList>

<Heading id="OZe6iGQAJc7uOSf4zcX2fP" level="3">
  6.2 时间轴首页（年/月/日）  【参考 群晖 Photos】
</Heading>

<Code id="Tu0JYhqAe7Mf7ellXhGvHb">
  ```
  +--------+-----------------------------------------+---------+
  | 侧栏   | 顶栏: 搜索 | 视图(年/月/日/全部) | 上传 | 筛选 |
  | 时间轴 |-----------------------------------------| 右栏    |
  | 相册   | 照片网格（虚拟滚动，懒加载缩略图）       | 选中项  |
  | 人物   | [缩略图][缩略图]... (360照片角标)        | EXIF    |
  | 地点   |                                         | GPS     |
  | 标签   | 年→月→日 钻取；悬停显示日期              | 标签    |
  | 文件夹 |-----------------------------------------| 分享▾   |
  | 设置   | 过滤: 类型/收藏                          |         |
  +--------+-----------------------------------------+---------+
  ```
</Code>

<BulletedList id="ooiHg4rdR7udKxi4LJo7mh">
  <Mark bold>功能点</Mark>：时间轴网格、年/月/日钻取、虚拟滚动（10 万+ 不卡）、筛选、360 照片角标、悬停日期。
</BulletedList>

<BulletedList id="Ul4NDK7h7G6rFpcqIo6p6v">
  <Mark bold>实现路径</Mark>：`vue-virtual-scroller`；按 `taken_at` 聚合年/月/日；缩略图 CDN；角标由 `ProjectionType=equirectangular` 标记。
</BulletedList>

<BulletedList id="sQZzDnyQZwDhdPnziZqSge">
  <Mark bold>参考</Mark>：群晖 Photos 的「时间轴」年/月/日视图与网格浏览体验。
</BulletedList>

<Heading id="9Kur5c2TTIVw94pwIHqskK" level="3">
  6.3 个人空间 / 共享空间切换  【参考 群晖 Photos（标志性双空间）】
</Heading>

<Code id="77Y8rCP9vVNBRBCODAbcBL">
  ```
  +--------------------------------------------------+
  |  [ ● 个人空间 ]   [ ○ 共享空间 ]                 |
  |--------------------------------------------------|
  | 个人空间: 仅自己可见，含 时间轴/相册/人物/地点/  |
  | 文件夹/收藏/回收站                                |
  | 共享空间: 以「共享相册」为主的异步协作区（邀请+角色）    |
  | （不要求多人实时互见；实时协同不在本期范围）        |
  +--------------------------------------------------+
  ```
</Code>

<BulletedList id="ncVKsTEAOqWi9ZvloHEX6p">
  <Mark bold>功能点</Mark>：顶栏切换个人空间/共享空间；个人空间仅本人；共享空间以「共享相册」为主（成员经邀请加入，按角色授权查看/贡献），<Mark bold>不要求多人实时互见</Mark>。
</BulletedList>

<BulletedList id="Uie1W8UHRQrV9WD2zM0y2N">
  <Mark bold>实现路径</Mark>：`media.space` 标记 personal/shared；共享空间成员表 `shared_space_members`；<Mark bold>成员与权限由本系统独立后台角色管理（不依赖 DSM 组）</Mark>，共享为异步授权而非实时状态同步。
</BulletedList>

<BulletedList id="KzJLrMH1BQKMOxYAQ3KY7v">
  <Mark bold>参考</Mark>：群晖 Photos 的「个人空间 / 共享空间」双空间模型（区别于单一图库的 Apple/PhotoPrism）。
</BulletedList>

<Heading id="FG7LsBS82H5FLROiF8GpDZ" level="3">
  6.4 相册：普通 / 智能 / 共享 / 收藏  【参考 群晖 Photos】
</Heading>

<Code id="aG2wjao6Gmy1OMIsGGVvwP">
  ```
  +-----------------------------------------------+
  | 我的相册: [普通: 家庭][智能: 海边2024][收藏]  |
  | 智能相册编辑: 条件(人物/标签/日期/拍摄地/     |
  |              相机镜头/文件类型/评价) → 自动聚合 |
  | 共享相册: [团队出游] 成员贡献/评论             |
  +-----------------------------------------------+
  ```
</Code>

<BulletedList id="taDA7VhRSlowuzsYuNIrZs">
  <Mark bold>功能点</Mark>：普通相册（手动）、智能相册（按条件自动物化：人物/标签/日期/拍摄地点/相机镜头/文件类型/评价）、收藏（特殊相册）、共享相册（成员可贡献、<Mark bold>可评论</Mark>（支持嵌套回复））。
</BulletedList>

<BulletedList id="ttCnBctxRJTmbtKDvkSmuD">
  <Mark bold>实现路径</Mark>：`albums`/`album_items`（`albums` 含 `description` 字段）；智能相册存查询条件，索引时物化；共享相册表 + 成员/评论（`album_comments` 表，含 `parent_id` 支持回复）。
</BulletedList>

<BulletedList id="WfeuxRZjDdiUpNCfSRDOqS">
  <Mark bold>参考</Mark>：群晖 Photos 的相册体系（普通/智能/共享/收藏），智能相册条件与群晖一致。
</BulletedList>

<Heading id="qcRCaVaUU8fVtuTW2GeBRg" level="3">
  6.5 人物识别与命名  【参考 群晖 Photos】
</Heading>

<Code id="xmNxD5kxPR9FODHJ7ZN9dY">
  ```
  +-----------------------------------------------+
  | 已知: [张三][李四][旺财(宠物)]... 未命名:[?][?]|
  | 操作: 命名 / 合并 / 拆分 / 隐藏(Hide People)   |
  | 选中人物 → 该人全部照片（时间轴）             |
  +-----------------------------------------------+
  ```
</Code>

<BulletedList id="KTOuTC55kUL5Vz5UNxcFB1">
  <Mark bold>功能点</Mark>：人脸+宠物识别、命名、合并/拆分、隐藏（Hide People）。
</BulletedList>

<BulletedList id="qCCgrU6GJK1AbJjXvujLKb">
  <Mark bold>实现路径</Mark>：OpenCV Zoo `YuNet`（检测）+ `SFace`（识别，128 维，Apache-2.0） → pgvector 近邻聚类；宠物走同类模型；命名存 `people`。
</BulletedList>

<BulletedList id="3dXzP8MtjOKILULeExjHKX">
  <Mark bold>参考</Mark>：群晖 Photos 的「人物」识别、命名、合并、隐藏（Synology 自带 AI 人物识别，机制一致）。
</BulletedList>

<Heading id="QW0JgCgyHVgS6W1Rrmw4cj" level="3">
  6.6 地图模式（全屏交互视图）  【新增大功能点；参考 群晖 Photos 地点地图 → 增强参考 PhotoPrism 3D】
</Heading>

<Code id="4xJ5nyyeSDeIfylFU4idx8">
  ```
  +--------------------------------------------------------------+
  | [图层▾: 矢量|卫星|3D]  [底图▾: 自动|高德(中国)|OSM(国际)]     |
  | [🔍 模糊搜索地点: ____ ]            [⚙ 设置: 滑块位置/侧栏] |
  |==============================================================|
  |                                                            ||
  |      [世界地图默认]  聚合密度圆点 → 点击展开缩略图           ||
  |      滚轮缩放至城市级（优先高亮媒体最多城市）                ||
  |                                                            ||
  |==============================================================|
  | [时间轴滑块 ▭▭▭●▭▭▭▭ 2019—2026]   (位置可设上/下)           |
  | [筛选栏: 类型▾ 人物▾ 标签▾ 时间▾ 地点范围]  (侧可设左/右)    |
  +--------------------------------------------------------------+
  ```
</Code>

<BulletedList id="C3YHUAQmSGOv3Kp9hRnkSe">
  <Mark bold>功能点（新增大功能点「地图模式」）</Mark>：

  <NumberedList id="qXoReqDGFzU9gmpIQWHR4o">
    默认世界地图，可缩放，<Mark bold>精度至少城市级</Mark>；<Mark bold>优先显示库内照片/视频最多的城市级区域</Mark>（按 GPS 聚类计数排序高亮）。
  </NumberedList>

  <NumberedList id="j78T4fXcF96e99Nz7qvPLC">
    <Mark bold>时间轴滑块</Mark>：地图上方或下方（UI 可配置）；显示「当前筛选条件 + 当前可视区域内」媒体的时间跨度；拖动或点击可定位到年/月/日或任意时间段（默认不限），即时刷新地图缩略图。
  </NumberedList>

  <NumberedList id="8UnhQ3umyGsDlSug1tyfav">
    <Mark bold>筛选栏</Mark>：地图左或右侧（UI 可配置）；按 类型/时间/GPS 标签/人物/标签 组合筛选，支持模糊匹配（ILIKE）。
  </NumberedList>

  <NumberedList id="5NzPhPDWYD0DEQ6wh83df0">
    <Mark bold>地理模糊搜索定位</Mark>：同时支持中国（建议 <Mark bold>高德 Amap</Mark> API，密钥手工设置）与国外（OSM/MapLibre）地图资源，可一键切换底图；支持中文地名模糊搜索并飞行定位。
  </NumberedList>

  <NumberedList id="QZ9XtprZ9WIWMAKSHTLhDS">
    矢量/卫星/3D 建筑切换、点击聚合点展开该地媒体（继承原地点地图能力）。
  </NumberedList>
</BulletedList>

<BulletedList id="CrVbRxRyk8kpqnbz8HpHCF">
  <Mark bold>实现路径</Mark>：MapLibre GL + 矢量瓦片（国际 Planetiler/OSM 或 Maptiler）；<Mark bold>中国底图经瓦片代理走高德</Mark>（API key 后台可配，前端不暴露密钥）；`supercluster` 按 `bbox`+`zoom` 动态聚合；后端 `/map/items` 按 `gps(POINT)`+`taken_at` 联合查询；`/map/timeline` 返回时间桶直方图；模糊搜索经高德(中国)/Nominatim(国际)；照片 GPS 多为 WGS-84，叠加高德需 <Mark bold>WGS-84↔GCJ-02</Mark> 转换；UI 偏好存 `user_ui_prefs`，底图/密钥存 `system_map_config`（见 DDL §2.5）。
</BulletedList>

<BulletedList id="AcWA7TvUoL6AeGJ4xn0KFe">
  <Mark bold>参考</Mark>：群晖 Photos 地点地图（基础 GPS 聚合）为起点；PhotoPrism 3D 矢量/卫星地图（付费增强）为升级项；本页为在两者之上<Mark bold>新增大功能点</Mark>——全屏交互地图模式（时间轴+筛选栏+模糊搜索+中外国界可配）。
</BulletedList>

<Heading id="BA4RiVaSTtsR2pWHyaBVKM" level="3">
  6.7 标签（用户标注 + AI 自动）  【用户标注参考 群晖 Photos → AI 自动参考 PhotoPrism】
</Heading>

<Code id="4GUmTnMCU7NON3Sb62XvPF">
  ```
  +-----------------------------------------------+
  | 我的标签: [旅行][会议][家]（手动加）           |
  | AI 自动: [海滩][日落][狗][建筑](可确认/移除)   |
  +-----------------------------------------------+
  ```
</Code>

<BulletedList id="Cxs0anMccdHL429QPLty6m">
  <Mark bold>功能点</Mark>：用户手动打标签；AI 自动物体/场景标签（可确认或移除）；标签筛选。
</BulletedList>

<BulletedList id="synjiCofUsvYHsNlEoq8tC">
  <Mark bold>实现路径</Mark>：`tags` + `media_tags`；AI 标签用分类/检测模型（PhotoPrism 式自动标注）写库待确认。
</BulletedList>

<BulletedList id="kFaMPm3pDLqrXoc6UL2Gpr">
  <Mark bold>参考</Mark>：群晖 Photos 支持用户标签；自动物体/场景标签参考 PhotoPrism 的自动打标能力（群晖仅基础人物 AI）。
</BulletedList>

<Heading id="lzNDW1E30iE5BTeTnXNQUz" level="3">
  6.8 文件夹视图（保留目录结构）  【参考 群晖 Photos（标志性能力）】
</Heading>

<Code id="VcxAThP0jaYOojaRj06T4W">
  ```
  +-----------------------------------------------+
  | 文件夹树: 照片/2025/冰岛/ | 日本/             |
  | 选择目录 → 该目录下媒体网格（按原路径）        |
  | 支持: 添加到相册 / 分享 / 批量操作             |
  +-----------------------------------------------+
  ```
</Code>

<BulletedList id="5sDTncjBfu34xKIi33VV6k">
  <Mark bold>功能点</Mark>：按原始目录树浏览媒体，保留 DSM/SMB 导入时的物理路径；目录可作为相册源。
</BulletedList>

<BulletedList id="SMBEOpugW6bokZRyg4p3GC">
  <Mark bold>实现路径</Mark>：`media.folder_path` 存相对路径；前端树组件 + 路径前缀查询；与「时间轴」并列入口。
</BulletedList>

<BulletedList id="ZVkCdEfkT7KaNtvASXHVoa">
  <Mark bold>参考</Mark>：群晖 Photos 的「文件夹」视图（保留 NAS 目录结构，区别于仅时间轴组织的 Apple/PhotoPrism）。
</BulletedList>

<Heading id="yBN8fdPIY8sArUjg0kkpQj" level="3">
  6.9 筛选与搜索  【基础筛选参考 群晖 Photos → 高级/自然语言参考 PhotoPrism/自研】
</Heading>

<Code id="FCekuRvgQU9WAYuzo05Ch9">
  ```
  +-----------------------------------------------+
  | 筛选: 类型▾ 收藏▾ 人物▾ 标签▾ 日期▾ 拍摄地▾   |
  | 搜索框: "Maya 穿红衫玩滑板" / "去年冰岛"      |
  | 结果: 联合筛选 + 语义召回                      |
  +-----------------------------------------------+
  ```
</Code>

<BulletedList id="dxQ8m7Rbr24sAejXixf5YR">
  <Mark bold>功能点</Mark>：条件筛选（类型/收藏/人物/标签/日期/拍摄地/相机）；关键词搜索；自然语言搜索（人物/地点/事物/时间）。
</BulletedList>

<BulletedList id="zX7g9RJZkqMWLBXUBzKpBl">
  <Mark bold>实现路径</Mark>：PG 全文 `tsvector` + pgvector 语义 + WHERE 拼接；视频瞬间用帧级索引。
</BulletedList>

<BulletedList id="LJ6UMJEgHWBQQQG1NOlACH">
  <Mark bold>参考</Mark>：群晖 Photos 的基础筛选与搜索；高级筛选/自然语言参考 PhotoPrism（群晖原生无 NL 搜索）。
</BulletedList>

<Heading id="PD2oYqHGdkSpheeBqKFKRO" level="3">
  6.10 照片 / 视频查看器 + 幻灯片 + 基本编辑  【参考 群晖 Photos】
</Heading>

<Code id="jplAXCKeHOZiHetPoNu47c">
  ```
  +----------------------------------+------------+
  |  大图/视频 / 360 球面照片          | 信息面板  |
  |  [←][→] 缩放/旋转 全屏 幻灯片      | 拍摄时间  |
  |  视频: 播放/进度/倍速              | 相机参数  |
  |  基本编辑: 旋转 / 裁剪 / 自动增强   |   机身:   |
  |  评级: ★★★☆☆                      |   镜头:   |
  |  操作: 收藏 / 下载 / 分享 / 加相册  |   焦距/光圈/ISO/快门 |
  |  实况照片: 照片+短视频配对切换播放   |   GPS 地图钉|
  |                                  | 标签/人物 |
  |                                  | 视频参数  |
  |                                  |   fps/码率|
  |                                  |   HDR/色域|
  +----------------------------------+------------+
  ```
</Code>

<BulletedList id="PdLRQQH3fBfJCpZNkHo2kQ">
  <Mark bold>功能点</Mark>：前后导航、缩放/旋转/全屏、视频控制/倍速、幻灯片播放、基本编辑（旋转/裁剪/自动增强）、<Mark bold>评级（0-5 星）</Mark>、收藏/下载/分享/加入相册、<Mark bold>实况照片配对播放（照片+短视频一键切换）</Mark>、<Mark bold>EXIF 信息面板（相机机身/镜头/焦距/光圈/ISO/快门/曝光补偿）</Mark>、<Mark bold>视频技术参数（fps/码率/HDR/色域）</Mark>。
</BulletedList>

<BulletedList id="QzZrG9WBYRxDjmkZvi7XWq">
  <Mark bold>实现路径</Mark>：编辑用 `libvips` 非破坏式（原图+sidecar 参数）；幻灯片用轮播；评级存 `media.rating`；EXIF 用 `exifread`（照片）/ `ffprobe`（视频）提取后落库 `media.camera_*`/`media.fps` 等字段；实况照片用 `live_photo_pair_id` 关联配对文件。
</BulletedList>

<BulletedList id="AgV2ikuOQFMgy0L6Rx0bU5">
  <Mark bold>参考</Mark>：群晖 Photos 查看器与幻灯片、基础编辑（旋转/裁剪/自动调整）；EXIF 信息面板参考群晖「信息」面板；评级/实况照片为本系统增强。
</BulletedList>

<Heading id="3SXk7bdTrxOe21vO7xxAMt" level="3">
  6.11 ★ 360° 全景照片球面查看  【参考 群晖 Photos（equirect 照片）】
</Heading>

<Code id="D9ZqIVJt4bXMlp2CpCQ3Is">
  ```
  +-----------------------------------------------+
  |  等距圆柱图映射到内翻球体（鼠标拖拽/触摸转动）  |
  |  [←旋转] [全屏] [分享]                         |
  +-----------------------------------------------+
  ```
</Code>

<BulletedList id="ffjovEDTYHN36WXkb1Otp0">
  <Mark bold>功能点</Mark>：对带 `ProjectionType=equirectangular` EXIF 的照片做球面渲染，拖拽/触摸转动视角。
</BulletedList>

<BulletedList id="P7UQrQ9ES2ulH3K6casefG">
  <Mark bold>实现路径</Mark>：Three.js `SphereGeometry` 内翻 + 纹理；与 6.12 视频引擎共用渲染内核。
</BulletedList>

<BulletedList id="PkcHeDABerzUfPXoAZYDA5">
  <Mark bold>参考</Mark>：群晖 Photos 原生支持 360 照片球面查看（基于 EXIF 投影标记）；视频版为自研扩展。
</BulletedList>

<Heading id="iQ7RQ06UBWoNKPie62B2Wb" level="3">
  6.12 ★ 360° 全景视频播放页（核心差异，必自研）  【群晖/PP 均无 → 自研】
</Heading>

<Code id="JJlZgGQnkzBDam2faNcUd0">
  ```
  +===============================================+
  | 顶栏: 标题 | 画质▾(自动/4K/2K/1080) | 分享 | 全屏|
  |-----------------------------------------------|
  |        等距圆柱视频纹理映射到内翻球体(球心相机)|
  |  [►/❚❚] [进度条─────●────] [音量] [倍速]      |
  |  [陀螺仪 开/关]  [VR 头显]  [FOV 缩放 ◀──▶]    |
  |  操作: 拖拽转视角 / 双指捏合缩放 / 设备倾斜转动 |
  +===============================================+
  ```
</Code>

<BulletedList id="vd8FpTsTGi88Ulqwy7XibV">
  <Mark bold>功能点</Mark>：球面渲染、视角交互（拖拽/触摸/滚轮 FOV）、<Mark bold>陀螺仪</Mark>（手机/PAD 跟随倾斜）、<Mark bold>VR 头追</Mark>（WebXR immersive-vr，左右分屏+头显姿态）、<Mark bold>自适应码率</Mark>（HLS 多档，按带宽推荐实时码流率）、微信 H5 链接分享。
</BulletedList>

<BulletedList id="uGRDwoCZgMUWGnehlbBNe7">
  <Mark bold>实现路径</Mark>：Three.js `SphereGeometry` 内翻 + `VideoTexture`；`OrbitControls`/自写 yaw-pitch；`DeviceOrientationControls`（含 iOS 13+ `requestPermission`）；`hls.js` 加载 `.m3u8`，`LEVEL_SWITCHED` 监测；WebXR `requestSession('immersive-vr')`；陀螺仪/WebXR 强制 HTTPS（Caddy）。<Mark bold>码率自适应</Mark>：`/api/bandwidth` 返回手动指定或自测得到的上下行带宽，`hls.js` 据此在 HLS 多档中选取初始/上限档位（详见 API §16）。
</BulletedList>

<BulletedList id="3R8lvvU9AYaDCWF1LdDmGO">
  <Mark bold>参考</Mark>：群晖 Photos 仅支持 360 <Mark bold>照片</Mark>、PhotoPrism 仅 equirect 球面无陀螺仪/VR/ABR；本页为唯一必须自研的核心模块（用户的全景视频刚需）。
</BulletedList>

<Heading id="X1XgpImCd5Wis2yHupb0kc" level="3">
  6.13 移动端 App / 自动备份 / PWA  【参考 群晖 Photos App】
</Heading>

<Code id="agQ3wgi8FufQDVJZkdOneq">
  ```
  +-----------------------------------------------+
  | 手机端: [自动备份 开] 备份源: 相册/截图/视频   |
  | 浏览: 时间轴/相册/人物/地点/收藏              |
  | 上传: 后台自动 / 手动选择                      |
  | 360 页: 默认开启陀螺仪 + 校准按钮             |
  +-----------------------------------------------+
  ```
</Code>

<BulletedList id="QmxFsySZa1BEZFoSLcQr5N">
  <Mark bold>功能点</Mark>：手机端自动备份（按相册/截图/视频源后台上传）、时间轴浏览、360 页默认开陀螺仪、PWA 安装主屏。
</BulletedList>

<BulletedList id="nypydyBnP3YMP2RkSApTDc">
  <Mark bold>实现路径</Mark>：复用 Web + `manifest.json` + SW 离线；备份用后台上传服务；陀螺仪校准 UI。
</BulletedList>

<BulletedList id="xjmD4wqDDmKk3wEvflfnQd">
  <Mark bold>参考</Mark>：群晖 Photos 移动 App 的自动备份与浏览体验（本系统用 PWA 替代原生 App，功能对齐）。
</BulletedList>

<Heading id="VAPK5KlNNcYTPYEtUB8AwU" level="3">
  6.14 分享管理 + 微信 H5  【群晖链接基础 → 参考 PhotoPrism + 自研 H5】
</Heading>

<Code id="oE6LQhUryFVryvgZqGsQkG">
  ```
  +-----------------------------------------------+
  | 我的分享: [相册A] 7天/密码✓/下载✗ [复制][撤销] |
  | [360视频B] 微信H5 永久 [复制微信分享] [撤销]   |
  +-----------------------------------------------+
  ```
</Code>

<BulletedList id="2uaRuLF56Uyckyq2FZBmOs">
  <Mark bold>功能点</Mark>：相册/文件链接、有效期、密码、是否允许下载、访问统计、撤销；<Mark bold>微信 H5 专用链接（非整文件下载，优先实现）</Mark>。
</BulletedList>

<BulletedList id="vNYFO20bm0zoVWEUR4OzGP">
  <Mark bold>实现路径</Mark>：`share_links` 表；反代对 `/s/<token>` 渲染轻量 H5（Vue 单页，不加载后台，支持自定义域名/IP/非标端口）；微信分享靠右上角菜单+OG 封面+复制链接/二维码；统计写 `share_access_log`。<Mark bold>小程序级分享为第二阶段目标</Mark>（同内容生成小程序卡片/中转页）。
</BulletedList>

<BulletedList id="SjsBXIVV8i54EhptFuYrgQ">
  <Mark bold>参考</Mark>：群晖 Photos 原生链接分享（有效期/密码/下载开关）；微信内 H5 体验与 360 交互需自研 H5 适配（群晖链接在微信内非 360 交互）。
</BulletedList>

<Heading id="MfcsSwC1l7ItIHF7yfI5Ny" level="3">
  6.15 管理后台（用户/共享空间/索引/转码）  【群晖 Photos 套件设置 → 细权限参考 PhotoPrism】
</Heading>

<BulletedList id="3l6DG6SAtEHmRIyXDdsZLO">
  用户/共享空间成员、配额、索引调度、转码队列监控、SSO、审计日志、备份。
</BulletedList>

<BulletedList id="x8bS33pPUe6twgaPvqdbhU">
  实现：RBAC 表；队列 BullMQ/Redis；审计 `audit_log`。
</BulletedList>

<BulletedList id="job052PlanNote20260920">
  规划状态（2026-09-20 增补）：管理端点（`/admin/*`，用户/角色/任务队列）后端已交付（Job000016）；
  **前端管理界面（AdminView）为计划内开发项 Job000052**；同批纳入后续正常开发计划的还有
  **Job000053 公开分享下载兑现**（`allow_download=true` 时公开侧真实下载，现为占位 403）与
  **Job000054 SSO/OIDC**（Keycloak/Authelia，见 §6.1 登录）。三项已经用户裁决确认为正式规划，见开发任务计划 Phase 5。
</BulletedList>

<BulletedList id="eWxJoofBK5qAmH9gMOfEuc">
  参考：管理控制台（索引重建、共享空间管理、用户/角色）采用<Mark bold>独立后台模式</Mark>（参考 PhotoPrism 多用户/管理控制台，不依赖 DSM）。
</BulletedList>

<Heading id="Ia7RkCUNHbjosuSFDi9mA1" level="3">
  6.16 工具箱（去重 / 恢复）/ 媒体类型  【去重参考 PhotoPrism → 媒体类型参考 群晖筛选】
</Heading>

<Code id="dV5D7h2KXvql5zOCYNjhpU">
  ```
  +-----------------------------------------------+
  | 工具箱: 重复项目 | 最近删除 | 已恢复          |
  | 媒体类型筛选: 视频|全景|RAW|截图|文档|实况    |
  +-----------------------------------------------+
  ```
</Code>

<BulletedList id="qkTmBTwQvnDCb8kVQRrB8t">
  <Mark bold>功能点</Mark>：重复项目检测合并、最近删除（回收站）、已恢复；按媒体类型筛选（视频/全景/RAW/截图/文档/实况）。
</BulletedList>

<BulletedList id="D01U3ZfN00mL4Y5Pw2NRhY">
  <Mark bold>实现路径</Mark>：pHash 感知哈希去重；回收站走软删；类型由 `media_subtype` 标记。
</BulletedList>

<BulletedList id="OyqIPo8RAhK2FzTIDKFTox">
  <Mark bold>参考</Mark>：群晖 Photos 有基础回收站与类型筛选；去重/恢复参考 PhotoPrism（群晖原生去重较弱）。
</BulletedList>

<Heading id="tfqhMAVXJNXn9CpV32dqAT" level="3">
  6.17 VR / AR 头显视图  【自研（群晖/PhotoPrism 均无）】
</Heading>

<BulletedList id="jX9zTnTOcQKeXs6NZFONxQ">
  复用 6.12 的 WebXR `immersive-vr`；头显浏览器打开 H5 → 沉浸模式；AR（`immersive-ar`）可选将 360 帧作环境贴图（需头显 AR 能力）。
</BulletedList>

<Heading id="05Ey38S41EzWSA4vSZd7h5" level="3">
  6.18 回忆影片 Memories（可选增强）  【自研（两者皆无）】
</Heading>

<Code id="vXrHKE8TglywzUSX0qRrLz">
  ```
  +-----------------------------------------------+
  | [回忆封面] 标题: "2025 冰岛" ▶ 自动混剪       |
  | 主题选取: 人物/地点/时间聚类 → ffmpeg 拼接     |
  +-----------------------------------------------+
  ```
</Code>

<BulletedList id="8KCj8R1xVt3VsXEHkzbNc1">
  <Mark bold>功能点</Mark>：自动回忆影片（基于人物/地点/时间的主题混剪）、章节/配乐可编辑、一键分享。
</BulletedList>

<BulletedList id="3yGeUTG452COuRpl5ZGPU7">
  <Mark bold>实现路径</Mark>：离线渲染管线（ffmpeg 拼接 + 转场 + 字幕）；主题选取用标签/向量聚类。
</BulletedList>

<BulletedList id="kBqoJvtQNzGjQikA9JUYJs">
  <Mark bold>参考</Mark>：群晖 Photos 与 PhotoPrism 均无 Apple 式 Memories；列为可选自研模块。
</BulletedList>

<Divider id="3OG3H1OIlyZo5RRUSfi49H" />

<Heading id="D0wiEo6iXAzFHrB5rgbwtd" level="2">
  7. 后端服务与 API 契约（主要端点）
</Heading>

<Table id="ergbivXqvTFssP1NG7WDa6" readonly rowHeader>
  <TableRow id="K9iYgkrukhSZHrFND2HHKW">
    <TableCell id="53QcclOSkwwvDjswpgnaFT">
      <Paragraph id="d7FH8ysXOHMtkWCcBdPP0i">
        服务
      </Paragraph>
    </TableCell>

    <TableCell id="cYDkSjTccXTrA2ydv8H4UU">
      <Paragraph id="rtGq964bYPyaX595rq9HsA">
        方法
      </Paragraph>
    </TableCell>

    <TableCell id="CXmOAeKCgisRpKdQg7Bsmy">
      <Paragraph id="SrJxtGcKLVQHwsqkp9eyz8">
        路径
      </Paragraph>
    </TableCell>

    <TableCell id="CuocIaTW9edisTER0pgCjf">
      <Paragraph id="JM7IkZluU9lkHe1ncFH9GR">
        说明
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="DzMVM8tEFPMCwed2vlDeqk">
    <TableCell id="7ZwgzFbj8HBjXD3DZ8lFWV">
      <Paragraph id="hca5bEYEUvG5421wfYTdLj">
        鉴权
      </Paragraph>
    </TableCell>

    <TableCell id="UnGGrBfA71xVM833KimHfR">
      <Paragraph id="1YCmxNj79cG5v7TnxB7TRt">
        POST
      </Paragraph>
    </TableCell>

    <TableCell id="jVRpPo2gs7bYL1enCtGpT1">
      <Paragraph id="Og5METc0toq1G6xpsp7i9i">
        `/api/auth/login`
      </Paragraph>
    </TableCell>

    <TableCell id="Ye15DVQ0KWfDqCKRWQKN7K">
      <Paragraph id="bPWO6T163ES3Y2kD3a7fWI">
        登录签发 JWT（独立后台账户，不对接 DSM）
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="kjcXbFFdfoVp30yQHtAuSO">
    <TableCell id="13Xz7UhHUIMYgPpWLpzhfH">
      <Paragraph id="nLSrmYc9NiEWB6nDlvwObq">
        鉴权
      </Paragraph>
    </TableCell>

    <TableCell id="rXIAsuuX4QKkkGSzoRrcEc">
      <Paragraph id="P8uQLfr4A20jqCKooJuzDs">
        POST
      </Paragraph>
    </TableCell>

    <TableCell id="nRCXCwTczfYeP2gPOjXJLb">
      <Paragraph id="YAz7gw2XJQfcuevG8Nhjfc">
        `/api/auth/sso/oidc`
      </Paragraph>
    </TableCell>

    <TableCell id="40VfLUWw928bcmCfGh3bRg">
      <Paragraph id="uDSL2BAt6ROocJkqEf6xhL">
        OIDC 回调
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="biWAP4n3AwH6Z8QSmEJaaq">
    <TableCell id="ZyRXQaKdWObnzYPsLVR2Qm">
      <Paragraph id="DkKscFhB3Tb6cbfeLSoFv9">
        鉴权
      </Paragraph>
    </TableCell>

    <TableCell id="sltpY83pceUkmvhiaA5Fvs">
      <Paragraph id="VC5vv669pADODykJrtQlEk">
        POST
      </Paragraph>
    </TableCell>

    <TableCell id="SHttHz6Lemx3kWVbFyQBJp">
      <Paragraph id="id1XphnkEeZxhDXapX9Oq9">
        `/api/auth/2fa`
      </Paragraph>
    </TableCell>

    <TableCell id="nu0zyBSD6oskFeVSK73uQ6">
      <Paragraph id="BJLLYUMkVy7RvKhHqkWVy4">
        TOTP 验证
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="0uIxXX13UfTeEq79hkDpdZ">
    <TableCell id="RszsMZQgjWVimU97bGaRms">
      <Paragraph id="7z1SAHB9DypNNHWXNf5NJJ">
        媒体
      </Paragraph>
    </TableCell>

    <TableCell id="19P2j2Fz5CBTIsD6OYR7l1">
      <Paragraph id="vnBNRc4FKqPDSoxntLi2oS">
        GET
      </Paragraph>
    </TableCell>

    <TableCell id="nWt2DJx7L0iqm5v35YIwq8">
      <Paragraph id="cLdKE7ph5wAiKty2Fe5mwg">
        `/api/media?cursor=&filter=&space=`
      </Paragraph>
    </TableCell>

    <TableCell id="bC7p88USvG4V0QmzgIyEf6">
      <Paragraph id="GfXhrhUMaajwMLHtaXTk6Q">
        时间轴分页（年/月/日/空间）
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="DV5FwiVnC6qP9LdwmLBGH8">
    <TableCell id="4hDo2crsbwBRLpfs4YecDf">
      <Paragraph id="BiJHa8hKJdXdsJtoZn0Qmz">
        媒体
      </Paragraph>
    </TableCell>

    <TableCell id="c31gfVGrU9rD3WUrv4Pd8p">
      <Paragraph id="8urxJWGBlzUa3T9EhpcZC0">
        POST
      </Paragraph>
    </TableCell>

    <TableCell id="ezo2KgDtLj6yIBliGV6X9k">
      <Paragraph id="QPGks34hsxkrINTXpkbPmC">
        `/api/media/upload`
      </Paragraph>
    </TableCell>

    <TableCell id="SAOyVKk9Iml7y8JNRTtLql">
      <Paragraph id="z8gF3PA3n8Ulurx7q6rYM8">
        上传文件（移动端备份/Web上传，分块续传）
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="eBD9UGm2KCsS9RJAg4HEL2">
    <TableCell id="IOJKqgWRogWJOZ29y2E8TS">
      <Paragraph id="cLetuIvEQBr6ZfJq29kDAd">
        媒体
      </Paragraph>
    </TableCell>

    <TableCell id="0ZVMNvnX6KgfAmeB99aX5H">
      <Paragraph id="eizRaTqGi8MFZJRVd70LyI">
        GET
      </Paragraph>
    </TableCell>

    <TableCell id="xO6fYJ8UhtjNoTUq35FNcD">
      <Paragraph id="5dcQMXThvo538X8xz4GtJC">
        `/api/media/:id/download`
      </Paragraph>
    </TableCell>

    <TableCell id="037lT1r5U1g7FbmpPSYXjG">
      <Paragraph id="sLhhFAmAqDH1ZjIjFpVip2">
        下载原文件（权限校验）
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="YmOf5QNRXpDJrt6iL4RZ07">
    <TableCell id="ub3rmSBYIzSS6N1v03It9t">
      <Paragraph id="mGNvddXu0tC6mRY8ntQvss">
        媒体
      </Paragraph>
    </TableCell>

    <TableCell id="9Xl9NMA8bik28jJ5V6qrT5">
      <Paragraph id="qSfpbG6BJZFmIIyXsRFcJB">
        POST
      </Paragraph>
    </TableCell>

    <TableCell id="y395xIwLc4ZnQ8ffEfgGos">
      <Paragraph id="KLNATxzbt7CD9rrI8dn4p4">
        `/api/media/:id/rate`
      </Paragraph>
    </TableCell>

    <TableCell id="Q7R6IFwql6umtCSYPAvlCI">
      <Paragraph id="hbVZmTAqV6GOOkw8vRtbmd">
        评级（0-5 星）
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="OotLAWNBs7cwYEUlaY1T9Z">
    <TableCell id="POafqOPs3CjVlyPFkueV3S">
      <Paragraph id="qCSMBFnbtYZwxMvIrhmkQy">
        媒体
      </Paragraph>
    </TableCell>

    <TableCell id="hPDviuuGUJKn5ACdhTrCD7">
      <Paragraph id="YcsqH6SabTGewxIs4c8oAS">
        POST
      </Paragraph>
    </TableCell>

    <TableCell id="0aPrHVTUxXKftEgYkVndJD">
      <Paragraph id="ZGgzDVtYfqsB4VXC1Lip0F">
        `/api/media/index`
      </Paragraph>
    </TableCell>

    <TableCell id="Ql3Wc0qSmbwgrjIb7XpOkT">
      <Paragraph id="6EUIWqWXISi7pMkp3mxmpA">
        触发索引（含 SMB 监听）
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="o0CerNnTq2IcyMbdMlmoao">
    <TableCell id="SYjZNcpEqSvUuPf5JGmJGO">
      <Paragraph id="xp6pZK3VenRJ260OXZpGlh">
        媒体
      </Paragraph>
    </TableCell>

    <TableCell id="x347yjqFHP0xz0vwXKMLd7">
      <Paragraph id="er8aLGxhgfZVmTbucy0zlO">
        GET
      </Paragraph>
    </TableCell>

    <TableCell id="0HqYeHKf8HXEVkW2M1CBvb">
      <Paragraph id="eG4T1ClKfH4HQzMwdetYxW">
        `/api/media/:id`
      </Paragraph>
    </TableCell>

    <TableCell id="QeHEsIeK36arKmxlvbuuE8">
      <Paragraph id="KUKRwsnLa1aWRxCkinXOLn">
        详情+元数据+EXIF+视频参数
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="t1oV9zcSYAE20rlD3QHwqT">
    <TableCell id="tNIl6WYOaqCg6SyaFFF0CK">
      <Paragraph id="PR4R8USr0KaZji7izyrVfF">
        空间
      </Paragraph>
    </TableCell>

    <TableCell id="aKRdBPFO3oB2HfTrOLdo8d">
      <Paragraph id="y0Kx1b4y8lL7AGZ0RYVQii">
        GET/POST
      </Paragraph>
    </TableCell>

    <TableCell id="avyO4YQ7t3po7YGl8cCZ70">
      <Paragraph id="24gNF79yGy0MEEVRznU4Mo">
        `/api/spaces` `/shared/members`
      </Paragraph>
    </TableCell>

    <TableCell id="W4LxLuS7HwORaNRvxxlPpQ">
      <Paragraph id="HiGusDHqYzltigIo8WvDDZ">
        个人/共享空间及成员
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="PZHdE7QzvBqsxbyMuzOSr4">
    <TableCell id="BnV5CYtLy4fvBrvl3kShlD">
      <Paragraph id="ISQtjL8glpjKKjEsBPhc3Q">
        相册
      </Paragraph>
    </TableCell>

    <TableCell id="u25HzfXmi6HuduAEv2hHZQ">
      <Paragraph id="RubPnPm5k60JyhiQwvn90I">
        POST
      </Paragraph>
    </TableCell>

    <TableCell id="wR1Tu9d05ekM313bRLm46b">
      <Paragraph id="8tACoDL71HjQKrDg3FosOq">
        `/api/albums` `/:id/items`
      </Paragraph>
    </TableCell>

    <TableCell id="dPPO1yTPAmMFKJ188bzr2L">
      <Paragraph id="2aSG6Gnexjo3aAskWNnPxL">
        普通/智能/共享相册
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="0sYOo0a6O2dbR0bXns0jLC">
    <TableCell id="Ov9oVG6ulZ7P9lgJrv2xdz">
      <Paragraph id="WflGQfiY28Fqonu9dbhVL4">
        相册
      </Paragraph>
    </TableCell>

    <TableCell id="d5FBdPYU7WnPqAIgO1eEk2">
      <Paragraph id="sW0HnEx9XIrPBw8HAkHhCJ">
        GET
      </Paragraph>
    </TableCell>

    <TableCell id="Jf2joUJJLIs4E46lm3m04G">
      <Paragraph id="RYsd0c0fQllDg9ynpJLx9l">
        `/api/albums/:id`
      </Paragraph>
    </TableCell>

    <TableCell id="EsLBjOxFULuJESJfG72qDv">
      <Paragraph id="hW6HSoj5AmjKa7s8gR13ri">
        相册详情（含 description）
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="1HeuNTcUG0bqZrK2zNY0dB">
    <TableCell id="SMWk0VyPVNRroW5zTHiOF7">
      <Paragraph id="8W6rbt7j5NPunHQ1Wq3DUu">
        相册
      </Paragraph>
    </TableCell>

    <TableCell id="NrdX5Klu5U3NyBShQbgwEL">
      <Paragraph id="PEAgadEVk1xcpynGlG93DA">
        GET/POST
      </Paragraph>
    </TableCell>

    <TableCell id="Gv7TBHGA4x5EZal4zLpXVo">
      <Paragraph id="1mKmNMHnxOy2vKfyjH0m7b">
        `/api/albums/:id/comments`
      </Paragraph>
    </TableCell>

    <TableCell id="1XVeTMgaJcb4uohAuDW06N">
      <Paragraph id="M0DH5n1RqfsFAZFMiR5Ssw">
        评论（含嵌套回复）
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="drWh0k9jGSLMnT3WgZZ5Pw">
    <TableCell id="nbcJhMQtLUEJ3bK1QCAnCD">
      <Paragraph id="hmWdarnTZfgNyiKJaLyOZS">
        人物
      </Paragraph>
    </TableCell>

    <TableCell id="jXbD1MD4bI6wHNtE1zGTiD">
      <Paragraph id="sLYBAUB3RJy5GmkClhkqHA">
        GET/POST
      </Paragraph>
    </TableCell>

    <TableCell id="pCLttH6fC0F5j5KtNNewp8">
      <Paragraph id="RNtrUoFPNGR6IkjhBM8ZOE">
        `/api/people` `/cluster`
      </Paragraph>
    </TableCell>

    <TableCell id="OVg7DsbBMlB8K0jYWjAAYm">
      <Paragraph id="EOnO4OgHEaRbGDZBvddbEf">
        人物宠物+合并
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="TnDoepuC5PbwwX57jw0Vk1">
    <TableCell id="IATQSccigERnsevihas7XS">
      <Paragraph id="XrZc0bhNlroVOSqSdiX59q">
        地图模式
      </Paragraph>
    </TableCell>

    <TableCell id="GDYsQrVyyuKpvQZ225Sh5O">
      <Paragraph id="ibsMlkmZ103sk8PjiBqKKL">
        GET
      </Paragraph>
    </TableCell>

    <TableCell id="MRo2grk4A5dTrWGOd6DEbG">
      <Paragraph id="9EylYkIXrkTARsi75ox6z0">
        `/api/map/items?bbox=&time=&filter=`
      </Paragraph>
    </TableCell>

    <TableCell id="cXbgHPJnam155DdC5AT2o9">
      <Paragraph id="pF2FheQee2mwgec0XotuSW">
        可视区聚合点+缩略图
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="Wfi7cLUBTswkbW41WrSFel">
    <TableCell id="MHVkcgHIhJa781eS8RtWpZ">
      <Paragraph id="4cqQEc2rZnKT7y4M2WPfTb">
        地图模式
      </Paragraph>
    </TableCell>

    <TableCell id="4iIlDc0PPQLXkm79Zkv7ri">
      <Paragraph id="PBCfS5k7S2GSwmgBXdQ7cH">
        GET
      </Paragraph>
    </TableCell>

    <TableCell id="le01szFtTqDErBzmcNPqSX">
      <Paragraph id="65rXywpCjMVqo5FkqOdsvl">
        `/api/map/timeline?bbox=&filter=`
      </Paragraph>
    </TableCell>

    <TableCell id="fB7OGW4quZpSJmtLhNGd7g">
      <Paragraph id="xShehF00NMmqS0jJBONDF7">
        时间跨度直方图
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="MsxVqQ38zbkY7MbshTONW6">
    <TableCell id="sa6UlCP4VlWpON7U6XTKb8">
      <Paragraph id="LxyhTjVZhYudCxtYZ0NuvK">
        地图模式
      </Paragraph>
    </TableCell>

    <TableCell id="s0lzUaJoiMj7v4ZoXwTLOY">
      <Paragraph id="HCRBvU72dI6VsPsebuBVIf">
        GET
      </Paragraph>
    </TableCell>

    <TableCell id="Agcg4eC1awvE6pbHXiYZS1">
      <Paragraph id="viiK1ttyGgQzmZO5lB9tuQ">
        `/api/map/search?q=`
      </Paragraph>
    </TableCell>

    <TableCell id="NQdXb59AxYuXedgylzfZCw">
      <Paragraph id="aUqrDBnuGqfljdRO907AYl">
        模糊地理搜索（中/外）
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="e3cncAWzRNoR9eGzHY89G3">
    <TableCell id="CmPSqtjM6U4GC0dBbrG340">
      <Paragraph id="L2m0eesKbWpDbvMiRbnXyA">
        地图模式
      </Paragraph>
    </TableCell>

    <TableCell id="ae8cLYq8nFuXmtKnVcH2pe">
      <Paragraph id="soHWH2nyrVdpcFyKinS845">
        GET/PUT
      </Paragraph>
    </TableCell>

    <TableCell id="W5rdG3zJ2ZaCgqlmZOqRar">
      <Paragraph id="8RxKfU0D1YSyTSJqo6UBV9">
        `/api/user/ui-prefs`
      </Paragraph>
    </TableCell>

    <TableCell id="MQzWEH1slaBdXfhnkOOuLZ">
      <Paragraph id="CHYY6stRcUtVx7kEj6MmQL">
        滑块位置/筛选栏侧/默认底图
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="UAKnRsmldmidHa8S6HyTnx">
    <TableCell id="TdxyvD4xp2W2pctIGX48o0">
      <Paragraph id="BFHKLQcZtBqjAPMgGSt4kA">
        标签
      </Paragraph>
    </TableCell>

    <TableCell id="7X2jrU0pJhWP3BlHOH6hae">
      <Paragraph id="a2ksRRtU2nfIGJmv4CoqU6">
        GET/POST
      </Paragraph>
    </TableCell>

    <TableCell id="EEN1CBCVBd0MTCAMZKqogt">
      <Paragraph id="r8f1FZD6M8QCyjLZRFsrTQ">
        `/api/tags` `/:id/apply`
      </Paragraph>
    </TableCell>

    <TableCell id="B4VZnD6d1n5WGYVDiLdwfp">
      <Paragraph id="cVD5SvpCrDaw3adFE1MuXu">
        用户/AI 标签
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="4GJRmDdEeIEvNcwHuhEHKo">
    <TableCell id="1uzzMtflHG1jJAMSDrXJ08">
      <Paragraph id="AXjiESZZPrxVy76auYwlUp">
        文件夹
      </Paragraph>
    </TableCell>

    <TableCell id="Ig8JIUyAfkX0WTGn3LElqc">
      <Paragraph id="eiYORPCie5O81zkC1YOXjj">
        GET
      </Paragraph>
    </TableCell>

    <TableCell id="Rj3JNol0HXXdUXTDZg4fGO">
      <Paragraph id="AtIz2FBj1bR5pmDIAxnuNM">
        `/api/folders?path=`
      </Paragraph>
    </TableCell>

    <TableCell id="4o9vyH2zdVpzUq7tuVu9ek">
      <Paragraph id="WGsHHWAmYKN2FRY7k9OUyX">
        目录树与内容
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="YVngzLe4DTBIRaIpq9psWm">
    <TableCell id="NvZSz6zCM6SqovQQkgz3Ma">
      <Paragraph id="OWQG2hWTBZMozJkLrNrvGG">
        WebDAV
      </Paragraph>
    </TableCell>

    <TableCell id="2M0RpHiD3j5xKXzBTdpiWL">
      <Paragraph id="Y1ywYI9OltzDqpr42cX5zO">
        GET/PUT/PROPFIND/MOVE/DELETE
      </Paragraph>
    </TableCell>

    <TableCell id="etb1kzIVLfygm6bbiiEcez">
      <Paragraph id="ryYuqeJzQ7TM5x9yfRuhJ3">
        `/dav/:path`
      </Paragraph>
    </TableCell>

    <TableCell id="9JAky8FcKKzDbGMN4YC7hQ">
      <Paragraph id="j1p8RD4VjKYfjAvV9KkK40">
        媒体直读直写（替代 SMB）
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="23JrrKliiF5Y1ffsLL1vjS">
    <TableCell id="tNiw85kE6tQgG51cCVFNNA">
      <Paragraph id="zAi1jVIPY6nW6swVa93zG9">
        分享
      </Paragraph>
    </TableCell>

    <TableCell id="9xcrVY6XkaiP6ryFlBbXPa">
      <Paragraph id="OS6l78H4M6cDNZRB4nAXTi">
        POST
      </Paragraph>
    </TableCell>

    <TableCell id="Rb2v3nDuafbxAE8aeKUqRJ">
      <Paragraph id="UrRQS0dfDbCpbLW4f097AX">
        `/api/share/album` `/media`
      </Paragraph>
    </TableCell>

    <TableCell id="w0x6jJdg4Pp3xfy6Zpmq2p">
      <Paragraph id="qtZPvKHleFEtFAfvu6fATv">
        生成链接
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="1RlD8je3ASECuZhL1ZWZSj">
    <TableCell id="RGBYiEUSSWFk86TKfwL0zZ">
      <Paragraph id="7tEuzWWSHmR7P5C89HPcTS">
        分享
      </Paragraph>
    </TableCell>

    <TableCell id="YeiztSSkc0WYT7v1BK1hMd">
      <Paragraph id="P8UbwwainEFmVpIQXSJymb">
        GET
      </Paragraph>
    </TableCell>

    <TableCell id="Sml0vMGc2OvphDSnC6n0NF">
      <Paragraph id="qeaj8pGLCCYLxnv5uAvfxh">
        `/s/:token`
      </Paragraph>
    </TableCell>

    <TableCell id="LEZyQffEVapeWPiDUg1i5v">
      <Paragraph id="d21pSNDFj8F2ju9DXbKq7s">
        H5 渲染（访客）
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="xWVUJenQGf7cf3ne1VZGhk">
    <TableCell id="cSw9PimWGTtgQiOE3axg8c">
      <Paragraph id="8ouj9WghmxPE75Ru50EGa3">
        分享
      </Paragraph>
    </TableCell>

    <TableCell id="TfR7hHuaYdFgfgYGY58lkv">
      <Paragraph id="RjTozkkyKaHNimyPHkVIfn">
        GET
      </Paragraph>
    </TableCell>

    <TableCell id="We8DGRDY5arb7ZoFzqQwQg">
      <Paragraph id="ofkKrXFlooUtQAnO8dLZMZ">
        `/s/:token/playlist.m3u8?key=`
      </Paragraph>
    </TableCell>

    <TableCell id="u4hn9OOZhE5LIB9CBXm9G4">
      <Paragraph id="mokwvgjrsgYE3djxcx9Ym1">
        HLS 播放（?key= 传解锁令牌）
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="SVWC1ZKYYkjLZ9Ivv62SIr">
    <TableCell id="u2U4dtww3ZhkbaEle74fmC">
      <Paragraph id="pQUiXM3s1uGCOPtDUP7XuZ">
        分享
      </Paragraph>
    </TableCell>

    <TableCell id="5K7f8fUUewj2VZbnkQhubW">
      <Paragraph id="QIiiv4d1ufDOPS6r5mAkTT">
        POST
      </Paragraph>
    </TableCell>

    <TableCell id="bMK93RypnETz5fPf0wCzXE">
      <Paragraph id="qGZyBwAxhYIN8jGmKbGsOZ">
        `/s/:token/bandwidth-test`
      </Paragraph>
    </TableCell>

    <TableCell id="gBkyjSHRwStEloyAYfJcWB">
      <Paragraph id="jZ8D9ctusfoWc8uXVcTZSC">
        访客带宽自测（无 JWT）
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="4KCaj6yFsBVJYyBAsZRYgv">
    <TableCell id="Ij5aXDhj4yIbGeHr3NXqEM">
      <Paragraph id="Gs8IgUhBIXbiG9BZLy6G6R">
        AI
      </Paragraph>
    </TableCell>

    <TableCell id="BuhhsFrq8AkwQk83lMnnaK">
      <Paragraph id="AW3K8277In9Ill0vtvIA8v">
        POST
      </Paragraph>
    </TableCell>

    <TableCell id="d0EnYqgMe6qQzCqzGPHf6q">
      <Paragraph id="A1jR52PEYGWR6Fc4Xqat0E">
        `/api/ai/faces` `/tags`
      </Paragraph>
    </TableCell>

    <TableCell id="5zuClY05QQtW277PW7SY9b">
      <Paragraph id="5GTdKkLhIpzMOBsFA4Di1h">
        人脸/自动标签
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="gAhgBOY0MffqPbGfGTKP84">
    <TableCell id="KgVRYLMhyEGv3TsTrHJaEp">
      <Paragraph id="PEooE5PY5fKUt9dEj7BBCu">
        转码
      </Paragraph>
    </TableCell>

    <TableCell id="foijtj6KgyfsFNq4hTf2Ql">
      <Paragraph id="o1fsoBFIMsy0X13o6astX5">
        POST
      </Paragraph>
    </TableCell>

    <TableCell id="kUhvHUI7CNgxPsAw7q2F7p">
      <Paragraph id="638j1DhGdkg5nXPNVl1Vq3">
        `/api/transcode/job`
      </Paragraph>
    </TableCell>

    <TableCell id="awWoQtuJRU4w2PzTOMloJz">
      <Paragraph id="NbJHR67Fe26QnJS0XHNbWg">
        提交转码
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="wLUUakV0qHdG8DAinyh5vN">
    <TableCell id="s84QWL0v8wQUZvejGEQxGT">
      <Paragraph id="53JvwV2JLy6CLOebb8yIxz">
        转码
      </Paragraph>
    </TableCell>

    <TableCell id="mHgPEU5EX8UcaiS4ufNON4">
      <Paragraph id="33xX6CDnbnFJCwwElqpR4E">
        GET
      </Paragraph>
    </TableCell>

    <TableCell id="qvVe72ZRSWgiPGDdW4hsKU">
      <Paragraph id="kvYXreNu09bxzynkDRNDG8">
        `/api/transcode/hls/:id/.m3u8`
      </Paragraph>
    </TableCell>

    <TableCell id="Wn6DY1OTchc14Hby3qqY6V">
      <Paragraph id="TP6HPm4IOUgcd3t7eOTS85">
        HLS 清单
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="kfCziPBJMEReAHqsyHwGge">
    <TableCell id="EAVYJssvDObePAJdh7UtrI">
      <Paragraph id="yZXVsamXYWsxxtDK7nzSRD">
        管理
      </Paragraph>
    </TableCell>

    <TableCell id="UYUTedzIvKSKULLJD5JMYj">
      <Paragraph id="u2bp62oJ2rV2TsjcP8xwTW">
        GET/POST
      </Paragraph>
    </TableCell>

    <TableCell id="EUhKwoy7kUYF4ornfVM3ua">
      <Paragraph id="Hp7BhYLHPfUBnpmeJ3INXO">
        `/api/admin/users` `/roles` `/index`
      </Paragraph>
    </TableCell>

    <TableCell id="fM9fFYLo7WKv51v5J5ddoN">
      <Paragraph id="37lNVNfVOhihlgbgJ7nuRy">
        用户/角色/索引
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="FPGeiC8mEgBiCUomNetOnn">
    <TableCell id="57Khjns7WafW0GRBPY2fQR">
      <Paragraph id="JLAE1ok7Yk0D92P6lYTe3Z">
        管理
      </Paragraph>
    </TableCell>

    <TableCell id="oqJ6Vzitm8wAkr8D5ZCXfV">
      <Paragraph id="LzGhsFqMcYpOu0A1uhAlhP">
        GET/POST
      </Paragraph>
    </TableCell>

    <TableCell id="boDwcBd2qIgERLpN9E0jre">
      <Paragraph id="NaTy43lXw2g1rOAAoMFEcG">
        `/api/compute-nodes` `/bandwidth`
      </Paragraph>
    </TableCell>

    <TableCell id="1bYWQqDqyX2sAzCssQ0Zf2">
      <Paragraph id="ZUfHEe5LGpJrULK0ek1LVf">
        算力节点/带宽配置
      </Paragraph>
    </TableCell>
  </TableRow>
</Table>

<Divider id="W89EgPg7hU2cfs3GLHbHUU" />

<Heading id="Fo3oCZDwzvkGkrSUmqqer2" level="2">
  8. 数据模型（核心表，PostgreSQL + pgvector）
</Heading>

<Code id="qQ4ieh111DOczQwuAX9dld" language="sql">
  ```sql
  media(id uuid pk, type enum(photo/video/360), space enum(personal/shared),
        path text, folder_path text, taken_at timestamptz,
        width int, height int, duration int, codec text, container text,
        media_subtype text[],            -- live/portrait/panorama/raw/screenshot/burst...
        is_360 bool, projection text,    -- equirectangular (360)
        gps geometry(Point,4326),        -- WGS-84（PostGIS 空间查询）
        place text, hash text, duplicate_of uuid,
        thumbnail_sm/md/lg text, hls_master text, filesize bigint,
        -- 相机/EXIF 元数据
        camera_make text, camera_model text, lens_model text,
        focal_length real, aperture real, iso int, shutter_speed text, exposure_bias real,
        -- 视频技术元数据
        fps real, bitrate int, hdr bool, color_space text, video_preview_at real,
        -- 用户评价
        rating smallint,                 -- 0-5 星
        -- 实况照片配对
        live_photo_pair_id uuid,
        -- 软删/回收站
        deleted_at timestamptz,          -- 非空=在回收站中
        embedding vector(512), owner_id uuid, created_at timestamptz, updated_at timestamptz);
  faces(id, media_id, bbox box, cluster_id, person_id, is_pet bool, confidence real, embedding vector(512));
  people(id, name, hidden bool, is_pet bool, cover_media_id uuid);
  tags(id, name, kind enum(user/ai), confirmed bool, color varchar(7));  -- 标签颜色
  media_tags(media_id, tag_id);
  albums(id, name, description text, type enum(manual/smart/shared/favorites), query jsonb, owner_id, space, cover_media_id);
  album_items(album_id, media_id, sort_key);
  album_comments(id, album_id, user_id, content text, parent_id uuid);  -- 评论（嵌套回复）
  shared_space(id, owner_id); shared_space_members(space_id, user_id, role);
  folders(id, path text, parent_id); folder_media(folder_id, media_id);
  memories(id, title, criteria jsonb, video_asset_path text);
  share_links(id, token, kind enum(album/media), target_id,
              expire_at, password_hash, allow_download bool, is_wechat bool,
              max_views int, access_count int);  -- 访问计数
  users(id, email, display_name, role_id, mfa_secret, mfa_enabled, app_password_hash, status, last_session);
  roles(id, name); role_permissions(role_id, perm);
  sessions(id, user_id, refresh_token_hash, ip, user_agent, expires_at, revoked);
  audit_log(id, user_id, action, detail jsonb, ip, at);
  index_jobs(id, kind, user_id, status, total, processed);  -- 含触发者
  transcode_jobs(id, media_id, node_id, kind, status, profile, result_path);  -- 含算力节点
  compute_nodes(id, name, kind, host, agent_token, codecs, has_nvenc, vram_mb, concurrency, status, last_heartbeat);
  bandwidth_profiles(id, scope, ref_id, up_kbps, down_kbps, source);
  bandwidth_tests(id, profile_id, up_kbps, down_kbps, latency_ms, measured_at);
  user_ui_prefs(user_id, map_slider_pos, map_filter_side, map_default_provider, map_default_zoom);
  system_map_config(id, china_provider, china_api_key_enc, china_tile_url, intl_provider, intl_tile_url);
  geo_cache(key, provider, result jsonb, expires_at);
  ```
</Code>

<Divider id="gvRJKjrSPvyEqBWzOwGr6U" />

<Heading id="gFpry8OFL2JcrNWFJkwEKO" level="2">
  9. 各功能点实现路径明细（标注参考来源）
</Heading>

<Table id="wrrQPUK2oaYgVrFy6T2IRz" readonly rowHeader>
  <TableRow id="vj37on93XrRGUh3HA3m3BY">
    <TableCell id="vDUAmVFjLYAzpib77UfypY">
      <Paragraph id="qFU672CsJ0DJHXhYMsVhzV">
        功能点
      </Paragraph>
    </TableCell>

    <TableCell id="JF4yU8ClWnpwuffRbH2ZWx">
      <Paragraph id="B2DMJ6glncMQyv26j4MBw7">
        参考
      </Paragraph>
    </TableCell>

    <TableCell id="CpUrhbV9iC53kyG8NzUq4s">
      <Paragraph id="XVs2pjscfh3PoKatmguO4l">
        实现路径
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="U4Cwv30JIP4Q37DHtWdqyl">
    <TableCell id="gDRHFRyx5Js01LgAlhElcj">
      <Paragraph id="UUii47wBSbMkIKM3Pus5jh">
        时间轴 年/月/日
      </Paragraph>
    </TableCell>

    <TableCell id="stEEpjBxhTfk2tm3a8qpA9">
      <Paragraph id="zZuoTA8BXuQP6sS8N1KfgR">
        群晖 Photos
      </Paragraph>
    </TableCell>

    <TableCell id="MWPTq9QIETrqQt8vnc7pTC">
      <Paragraph id="GYgASIv0oiPQuo5hbrRQS9">
        虚拟滚动 + 时间聚合
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="kmMO0Xt25icIAgzq3Fmor2">
    <TableCell id="2cqxwP51UcvIwzn4G243N7">
      <Paragraph id="WAaxGfnr4ZOaS8UthyDejK">
        个人/共享空间双空间
      </Paragraph>
    </TableCell>

    <TableCell id="4fiqZTaoQtfVFZ6L4wypIJ">
      <Paragraph id="81Zzk1g7m1vxsciK416NQw">
        群晖 Photos
      </Paragraph>
    </TableCell>

    <TableCell id="8NRyyqqQ55jjPaaoNnAqDa">
      <Paragraph id="IMh6mo7kgW6FxK3FYm7dEP">
        `space` 标记 + 成员表
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="y9ywbt3XDu2ef63wmhbyhZ">
    <TableCell id="KMyAxpfvg6sfVvDlDcRYQz">
      <Paragraph id="dff30gAv9JaDWpGlCp8ZJ4">
        文件夹视图
      </Paragraph>
    </TableCell>

    <TableCell id="2agaOCaTxdvG9kJirpuTsQ">
      <Paragraph id="Q3QeOgbZEV8hwQn70NnDaP">
        群晖 Photos
      </Paragraph>
    </TableCell>

    <TableCell id="xxnezNahIjJgpzUrXO7Xke">
      <Paragraph id="Fic1E4Vi6KMjADwl9LwChh">
        `folder_path` + 树查询
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="Z8VvS0EEfuO3wbiNVBWMde">
    <TableCell id="pgE3aIZieuGar7JpLT5sJi">
      <Paragraph id="0TzEcKXtlgJ2TqYZsBrDzg">
        相册（普通/智能/共享/收藏）
      </Paragraph>
    </TableCell>

    <TableCell id="7ZuGoAfvcPsoKaYUM3sJDs">
      <Paragraph id="7LRrMvuKMa6jWcV1LvW3Z1">
        群晖 Photos
      </Paragraph>
    </TableCell>

    <TableCell id="2CLt4rrm5EeoqCBsoc3z4R">
      <Paragraph id="AclDwV6IrqkKxViVyMUIyb">
        查询物化
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="sBaWlp8tqDUpGFgPE6uXYV">
    <TableCell id="46K4RsHqfPK4nJIheZiA02">
      <Paragraph id="u2KGQiBpIukYACPnBDjuFX">
        人物识别命名
      </Paragraph>
    </TableCell>

    <TableCell id="QzyGDdDztpcMXdeo8wR682">
      <Paragraph id="ka4bz64ErH2jAylC7twbIe">
        群晖 Photos
      </Paragraph>
    </TableCell>

    <TableCell id="pNlH28mTv2cCQNWqPMBM8c">
      <Paragraph id="O1sQ6q7KxTp1pCEcYtP804">
        YuNet + SFace + pgvector + 合并
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="e5aaPJWPIgMPjYQPD9EMIv">
    <TableCell id="XizIx4diHrAQYnhGyemHxF">
      <Paragraph id="H8NT5RJvv1n5nseQx0Cvfx">
        地点地图
      </Paragraph>
    </TableCell>

    <TableCell id="e43SkNDY805yCRMvSlrrF0">
      <Paragraph id="UIEToAIY8tqzIlaqMo04hz">
        群晖→PP
      </Paragraph>
    </TableCell>

    <TableCell id="WON53zDSJgQsLaYaMCqewQ">
      <Paragraph id="kVyYy31dwDwuMZST7xqnjC">
        MapLibre + 矢量瓦片 + 3D 挤出
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="KoT5lH5Epoe11CAeM5Efuj">
    <TableCell id="YqKxl3fVaYWSCbBOLDQCmT">
      <Paragraph id="idfpOYu9KLIFX53ZR2ZW3n">
        地图模式（全屏）
      </Paragraph>
    </TableCell>

    <TableCell id="CB5WYQVhvSRAi2qGC6HZdz">
      <Paragraph id="g56TvLYbUIhtWMmdu123Rx">
        （无）→自研
      </Paragraph>
    </TableCell>

    <TableCell id="jwkIdvAg1rDpwsXJ1jf9JE">
      <Paragraph id="VsJURWjneIaQDXYm2zSloM">
        MapLibre + 高德(中国)/OSM(国际) 双底图 + 瓦片代理
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="OAHWOSlFQ78NfNAw0vgbBK">
    <TableCell id="AogESnuZA5gfsh2z4PROBY">
      <Paragraph id="5BRFPTdgJ0C8Kkzv8znDre">
        城市级密度优先
      </Paragraph>
    </TableCell>

    <TableCell id="7APNjAfm1jLvwHOY4UsUgP">
      <Paragraph id="3n46gL7bEZ1pHHEBdG1ls8">
        （无）→自研
      </Paragraph>
    </TableCell>

    <TableCell id="PtRs0RusFCxKQCxWKkmCaM">
      <Paragraph id="0i7yNsJAYyJ47Cjhk7z9ay">
        按 GPS 聚类计数，优先高亮媒体最多城市级区域
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="zdjdx1y1OTyVVNgiDkDeZS">
    <TableCell id="PSoFtLOU47y8nOs6vbIbet">
      <Paragraph id="EjE6tqPnqaFYtPRKvpT4JH">
        时间轴滑块
      </Paragraph>
    </TableCell>

    <TableCell id="ickfoDFqzflM72FR8tfdKE">
      <Paragraph id="c1mYXWvxXMyCdBmhndV1yX">
        （无）→自研
      </Paragraph>
    </TableCell>

    <TableCell id="OLVavXJ27rjlhhDN6qDYfp">
      <Paragraph id="6leAzLGEipXsRXhjpO9Z60">
        taken\_at + bbox 直方图 + 时间窗查询
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="22AyFds7nNh3wvPWVjBam8">
    <TableCell id="TPQy17e7KYDhbuqgqn02xV">
      <Paragraph id="LfhoaOKGTJql289TQnskLi">
        筛选栏模糊筛选
      </Paragraph>
    </TableCell>

    <TableCell id="c85qsw6vORJDI0wMJxvmfM">
      <Paragraph id="jHURXPHK34euChHVQKHZNZ">
        群晖→PP
      </Paragraph>
    </TableCell>

    <TableCell id="xMuTZkTQPeLsuYXtHso30J">
      <Paragraph id="NXMEa65FrdsxxnzMKkxQRs">
        类型/人物/标签/GPS 组合 + ILIKE 模糊
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="iQN2raKGsz3htEeMWMHRCk">
    <TableCell id="24MpzCV3mO8iixcwmndiUF">
      <Paragraph id="urTYJ6eSFoN2yIRFkiXRgH">
        地理模糊搜索
      </Paragraph>
    </TableCell>

    <TableCell id="k91vUZe8jiGMRFoHLFyOkJ">
      <Paragraph id="13er077kZkLDnjxW4FTH2D">
        群晖→PP
      </Paragraph>
    </TableCell>

    <TableCell id="lOC7QW6MsL2YC851lj9Scm">
      <Paragraph id="zxrbNLt9NaOniyd3KpqbfL">
        高德(中国)/Nominatim(国际) + WGS-84↔GCJ-02
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="RuUkbm3HXrLsq7Odw6L7zj">
    <TableCell id="DIfv3PjCDVKv4Uyks6jskX">
      <Paragraph id="huqfbCmNry1ieiHDIPIVGe">
        标签（用户+AI）
      </Paragraph>
    </TableCell>

    <TableCell id="sejm1lCTMz1EL3jfx8XzKa">
      <Paragraph id="zJHJ5ITZfOJcAnK1UlAIUk">
        群晖→PP
      </Paragraph>
    </TableCell>

    <TableCell id="WeLTUhqt0eplPYSryDZIIU">
      <Paragraph id="e8TAULMVU3iKLioVmbDwLC">
        用户标注 + 自动分类模型
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="pQ8nTMCGxfxqbQStLt7LSP">
    <TableCell id="bkvhNSHZ2V6heU6GgjMoLN">
      <Paragraph id="1TC7ZZOl05dxbfgiJvy656">
        查看器/幻灯片/基本编辑
      </Paragraph>
    </TableCell>

    <TableCell id="4134nYGqhVNEQBhPLKFE9k">
      <Paragraph id="LKVykNcnICD8wGzB2XEkfR">
        群晖 Photos
      </Paragraph>
    </TableCell>

    <TableCell id="jtcyFxmILqchVUrrUAAF4b">
      <Paragraph id="bormyfBmHPE1sxl16ctcWf">
        libvips 非破坏 + 轮播
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="tP2NZnqgmn0h93PAnhDpld">
    <TableCell id="bL1ekOSAE3xlQqCgNYIzy5">
      <Paragraph id="pIpulxT4GfBiKZt4CJ7A8i">
        360 照片球面
      </Paragraph>
    </TableCell>

    <TableCell id="iVdezEVwBV5XsftnOephdE">
      <Paragraph id="HMdd423p2cpEch2fCsGh30">
        群晖 Photos
      </Paragraph>
    </TableCell>

    <TableCell id="3XJ9u52JfNE1iq1KeKNrMJ">
      <Paragraph id="bZuBix6y8Awfr0lq1aqVWM">
        Three.js SphereGeometry+纹理
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="dDll3nDMJQXXQFmsFrUAui">
    <TableCell id="mzhPseft2dzEJS5IJfDxR8">
      <Paragraph id="dpZXB7a4cBaAtoBX3lyNGM">
        360 视频球面渲染
      </Paragraph>
    </TableCell>

    <TableCell id="i2lsdFOMnbtet52r3nNBBt">
      <Paragraph id="f39HEVkIXqtdl7Zcqmtwfg">
        （无）→自研
      </Paragraph>
    </TableCell>

    <TableCell id="J8YGzrUMOejSFcnvszQN9H">
      <Paragraph id="AUnMsuGddqW0BHIOQFEafo">
        Three.js + VideoTexture
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="VNelvoY1z8do9LYDqaXDOS">
    <TableCell id="1BpTPi449576aekPvBAzbo">
      <Paragraph id="hMMEWx26oJ3pn5qfY6fGxA">
        陀螺仪
      </Paragraph>
    </TableCell>

    <TableCell id="q2jZJs66XotkSidLznNh2i">
      <Paragraph id="gxysZEI3ZUNXEbmLZDj1to">
        （无）→自研
      </Paragraph>
    </TableCell>

    <TableCell id="lcCP6L4fM7x3ILaZnMUacm">
      <Paragraph id="rvGSAXM5GCB3Q2ClFrbJD1">
        DeviceOrientation + iOS 权限
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="g4M3Lxqe2OA51nX3NWeaRN">
    <TableCell id="Viq8iR8HNURieWb3z5fXjF">
      <Paragraph id="kS0nVh5LfVwjO8qhDa4W0m">
        VR 头追
      </Paragraph>
    </TableCell>

    <TableCell id="klVCPmDxokvMYzav9x9Ol0">
      <Paragraph id="yDmrRT1n8Ncbb1xYG7jI8W">
        （无）→自研
      </Paragraph>
    </TableCell>

    <TableCell id="JiLFijgIjtoBmgR9pwIhbn">
      <Paragraph id="CjPR5JpdMYI6054R8x5a13">
        WebXR immersive-vr
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="f94t0wmAuOmcZ5sYnYTH1A">
    <TableCell id="kkzA0dJL7RnhPXOYuaxKw9">
      <Paragraph id="hQ3wJjUFeunh9dN0NpvtVz">
        自适应码率
      </Paragraph>
    </TableCell>

    <TableCell id="uCrfNhSV9UhfYK5AjNOD6y">
      <Paragraph id="5WibNeZs9j2UQznIlHxFey">
        （无）→自研
      </Paragraph>
    </TableCell>

    <TableCell id="RBESgpO1Dyz5A9TGXLWwve">
      <Paragraph id="Ce9rzwR5TeaRwvNT8rPJyx">
        HLS + ffmpeg 多档
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="0kQxnYbzhGWprzlD1JXHtQ">
    <TableCell id="hwvkHJz9C5DiKhc5JtSkFJ">
      <Paragraph id="kCVnxmuZQv40ux9oVukctD">
        自然语言/高级搜索
      </Paragraph>
    </TableCell>

    <TableCell id="RHphGIoFJFUoOJ7wk9cCWM">
      <Paragraph id="b2Aj9hqbd0Xwnvro7WaEZM">
        群晖→PP/自研
      </Paragraph>
    </TableCell>

    <TableCell id="zPWvzx1c72tQCYMWsWPuoq">
      <Paragraph id="zHclJWL8SgoJNUPmzJ1IDJ">
        全文 + pgvector + 视频瞬间帧
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="kKCuZMcMWcIfiNOZrrzwfE">
    <TableCell id="xz3X1fJurIxJE1XzrB6ZF1">
      <Paragraph id="tMge5lmgjVPhoHWdWQXbrm">
        微信 H5 分享
      </Paragraph>
    </TableCell>

    <TableCell id="fz1hitK2R4EmjoyCFtBY8G">
      <Paragraph id="SB2MN8a2eeHKNj2xzOwbBZ">
        群晖链接→自研
      </Paragraph>
    </TableCell>

    <TableCell id="wi2JUnX3fXi9UjIuTE10qE">
      <Paragraph id="JVBySdp8DLhqvDs9HVLyHP">
        轻量 H5 + OG + Caddy HTTPS
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="ZVo0Hi5SLtSdmqrHZtMTbN">
    <TableCell id="4FfBMfgrNEw0HHXYJtj2Dp">
      <Paragraph id="r2HLTwj1lPGZASnFhzP2lx">
        多用户/SSO/2FA
      </Paragraph>
    </TableCell>

    <TableCell id="ZMXynLhae7gJYYeV9T6L7X">
      <Paragraph id="KrqVxwLJhRWBjeVupaAJEN">
        独立后台（参考 PP RBAC）
      </Paragraph>
    </TableCell>

    <TableCell id="F8alErU38Q6QHfjwjOd6DS">
      <Paragraph id="lyNJ6t20LXD317LfU2Ozsh">
        RBAC + Keycloak + otplib
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="ZhPiFBHob07TUYrXuiL1Fs">
    <TableCell id="77fnFIocjSFTa9xixxuxkA">
      <Paragraph id="H0QubpDc4efL5IRfPA4LBV">
        保留目录结构/WebDAV
      </Paragraph>
    </TableCell>

    <TableCell id="BqNM1zqeCgYnN2j7CXtcvB">
      <Paragraph id="61juiP4ZdfQFwMNHG3DxaA">
        群晖文件夹→PP
      </Paragraph>
    </TableCell>

    <TableCell id="BtiQu8cRChy8BsIkHTtdAB">
      <Paragraph id="tdjQGllpDJvNmzRfL5noco">
        文件系统映射 + WebDAV 服务
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="seHJekdbmb3Bk4AaHOu5qi">
    <TableCell id="2a7LiAe7TjH1JZ8ldEwFps">
      <Paragraph id="c6r3Bio6lQq4AUKlS9qv5R">
        去重/恢复
      </Paragraph>
    </TableCell>

    <TableCell id="ikYhH33Giie6wLYQc5SjZQ">
      <Paragraph id="WuVlXhWXiegydLaMkLptbf">
        PP
      </Paragraph>
    </TableCell>

    <TableCell id="54ooThkfNmdvru4BoO6ZS8">
      <Paragraph id="KvHjeuD7sfsaegBdzhNaXK">
        pHash + 软删
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="K6HzZIPJmW85m67jU9mYhA">
    <TableCell id="xVXyZIufCCTMb7VUgcTNcz">
      <Paragraph id="qq6X5UR9Fiba61NG0gyUeI">
        回忆影片
      </Paragraph>
    </TableCell>

    <TableCell id="jJm6oDFxP8YehkhjSqrZOo">
      <Paragraph id="2RFxCvptiByyPtsjvbBqbq">
        （无）→自研
      </Paragraph>
    </TableCell>

    <TableCell id="tfCgUzM38zcHg8ClDsGbzG">
      <Paragraph id="LJTWsGwzNKtYNx2fLkh3kZ">
        ffmpeg 混剪 + 聚类
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="TXyYntiqSSJ0QdDM2P7OYN">
    <TableCell id="TLfgKFgv3tMvGg8p70YgHV">
      <Paragraph id="dDEINykVpzJ83SusHmTMB0">
        EXIF 元数据落库
      </Paragraph>
    </TableCell>

    <TableCell id="clnmtj2xd1xlIQkZePyiZF">
      <Paragraph id="rzmMoDrggICClkV4S91rtS">
        群晖→自研
      </Paragraph>
    </TableCell>

    <TableCell id="YTFUJ6ydUghtns8lQ95uHr">
      <Paragraph id="R1o0ghJh4s5O5xVxUhQFJQ">
        exifread(照片)/ffprobe(视频) 提取→media.camera\_\*字段
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="sOcEGwbrODScqtfEg0Bqfz">
    <TableCell id="5OuUc3jchE4Ppinj0u78Hr">
      <Paragraph id="ONprFBwyo16QUQXwTiJChd">
        评级/星级
      </Paragraph>
    </TableCell>

    <TableCell id="GkKW99hKKk3w3F83L6cHK7">
      <Paragraph id="9JzQSH2vV6gg7osmDsEWWW">
        群晖→自研
      </Paragraph>
    </TableCell>

    <TableCell id="e5z0rL923rYeM8gRcEmwdw">
      <Paragraph id="ZTfq4fgLdJ3UniMJ3VKSX9">
        media.rating(SMALLINT 0-5) + POST /media/:id/rate
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="Aiynrt8SFvyQHRHVxj9cu9">
    <TableCell id="5KR9jPU89MqPV4gNsDut0B">
      <Paragraph id="QvB7sC2S10ZON1EYUKykzQ">
        实况照片配对
      </Paragraph>
    </TableCell>

    <TableCell id="PUaJs2710rymaZLGRnOHR5">
      <Paragraph id="79FhOAKVdX77McxflMDTEN">
        群晖→自研
      </Paragraph>
    </TableCell>

    <TableCell id="KO2PwDfCoZVqQHhOsoTUjL">
      <Paragraph id="eRuW3HfOS7R1JSS4a6KKtD">
        media.live\_photo\_pair\_id 关联照片+短视频
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="Q58nnTrqP4rUcFdFtypTLX">
    <TableCell id="m6rp7VOB69qPF7sx8iXmYc">
      <Paragraph id="8BNZlpA8YGchGGUaBWVrfz">
        相册评论
      </Paragraph>
    </TableCell>

    <TableCell id="awdrgHw1ZFwuUaXImsYX36">
      <Paragraph id="8lKguif61fYRotPVdSFiOO">
        群晖→PP
      </Paragraph>
    </TableCell>

    <TableCell id="AjH5OMCIWc8JK2NAyx9LbD">
      <Paragraph id="rBAILBJU2fPavPsO5ZjGJj">
        album\_comments 表 + 嵌套回复(parent\_id)
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="qy52xgfjVx4si0S3adXTA4">
    <TableCell id="FTauFMtbLaQTqz6QCJIAHt">
      <Paragraph id="6y5JeioofE49lL23smtE5O">
        WebDAV 直读直写
      </Paragraph>
    </TableCell>

    <TableCell id="u9R6DyL0FYSCFBxjKZpdE8">
      <Paragraph id="IrtJpll2EkC7eKLwRpacPa">
        PP→自研
      </Paragraph>
    </TableCell>

    <TableCell id="ffy740Ow6AONuNDpQtxYIV">
      <Paragraph id="6FuFqYir7x7QCBRS2xy4EC">
        Go webdav 中间件 + folder\_path 映射
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="FK09RqVE66lLTQsDMOtPQV">
    <TableCell id="uMD3Wks2Fz5v7XCoG8q0at">
      <Paragraph id="zFA7oxZtj6Q6HbG8lfK5kN">
        文件上传/备份
      </Paragraph>
    </TableCell>

    <TableCell id="IDdwDdWobC5PAkXKzXn2mz">
      <Paragraph id="iwaHh1Of09FYA8WNYryliF">
        群晖→自研
      </Paragraph>
    </TableCell>

    <TableCell id="yz7GnPPvuircHNwcx8PBa6">
      <Paragraph id="67CWCfNsyT7V1XqzexMvRr">
        multipart 分块 + Content-Range 断点续传 + 异步索引
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="upDLR3wOPaLl6qFWu5iKZd">
    <TableCell id="bbBZBHmQdFkHX93dF7tl1x">
      <Paragraph id="Y7TfdnWcSXAPHNDRkV3Cmb">
        数据迁移工具
      </Paragraph>
    </TableCell>

    <TableCell id="NI2wrl3QuNV2SvtfPJAJb7">
      <Paragraph id="FLkDsfwckcZm8RLgZQ1o1B">
        （无）→自研
      </Paragraph>
    </TableCell>

    <TableCell id="sdhaPiRwWPuuZZq0GmmGyX">
      <Paragraph id="MEphlqmXRRHyIjErHJ2yKB">
        rsync+inotify 扫描群晖目录+sidecar→映射入库
      </Paragraph>
    </TableCell>
  </TableRow>
</Table>

<Divider id="P7W6ZhkhXkhu1sRCgz2Uq5" />

<Heading id="xNowqxYHAv9bE7rlgiEc2A" level="2">
  10. 分阶段实施路线与验收标准
</Heading>

<Table id="a8FP0ZxkyPLUv1My7xz032" readonly rowHeader>
  <TableRow id="XiWSW8upaEc0pWxsEUcpa6">
    <TableCell id="2XisGBBvA1ypP24CQgVdMF">
      <Paragraph id="KyPA5msH3mUdufIxY7VSQ6">
        阶段
      </Paragraph>
    </TableCell>

    <TableCell id="cm7qQgPudc7UXaJXYSRAGP">
      <Paragraph id="wxLr9CbboGDPu4nX8meBMA">
        交付
      </Paragraph>
    </TableCell>

    <TableCell id="GffMlB20WaLfGA1NdXJZjp">
      <Paragraph id="wFr2s0UoGTe9bsKC4rOsNw">
        验收
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="Zb9EKdHEjJswAoVC4OpBLJ">
    <TableCell id="sqDZ5IDrUmAegcR84gYNlg">
      <Paragraph id="uGUSpLBnztYX2soluZ5XUS">
        P0
      </Paragraph>
    </TableCell>

    <TableCell id="rjeW6G0qhng6p72QOvazKD">
      <Paragraph id="pYXsNVSdhUjHLuyCV8fwl3">
        时间轴+年日月+索引+缩略图+双空间+文件夹视图
      </Paragraph>
    </TableCell>

    <TableCell id="miZ2Or5ixDthlXtdwvyqiu">
      <Paragraph id="PqJnBjLQEBJFDK9EDcjAm9">
        群晖 Photos 级浏览体验
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="8WVhCcTSEUpizENGLoILy2">
    <TableCell id="8WfZm21RnOkRffXUsP6Nh9">
      <Paragraph id="qGw9oVpP918sCSroeTrWbS">
        P0
      </Paragraph>
    </TableCell>

    <TableCell id="9CKvfPI86IfDD7BcLiaKk3">
      <Paragraph id="BQFWUqnlUrcc0HOA0KGsCJ">
        ★ 360 播放引擎（陀螺仪/VR/ABR）
      </Paragraph>
    </TableCell>

    <TableCell id="ekfIAQZmze6S4YRwzAXV3r">
      <Paragraph id="IjPl8iPJF6pgEZXmUf34S3">
        iOS/Android/Quest 可交互
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="X1S8h2ZYl5cu9k1wHZcfQJ">
    <TableCell id="GAvIGPoE5RwLuNMGGvlDNj">
      <Paragraph id="Z1MKu8vtFYG9gtqzBGU5fR">
        P1
      </Paragraph>
    </TableCell>

    <TableCell id="oVAwWHbmIrrp5mABK4nZ8n">
      <Paragraph id="m5SeElXeOr1Bu8tlfl7kf1">
        相册(普通/智能/共享/收藏)/人物/地点/标签/文件夹
      </Paragraph>
    </TableCell>

    <TableCell id="vw2cMsCrBoZKadGgjcq9rG">
      <Paragraph id="a8CSMZGthFtteh6hpwuHtc">
        自动聚合准确
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="vpqwcEW3UI3Ii3CvGGfxSK">
    <TableCell id="LM7tMAJdBjbH09ElLgXKVF">
      <Paragraph id="fkShtMWutyAYJ1EGB838H6">
        P1
      </Paragraph>
    </TableCell>

    <TableCell id="0x7C6viJQVzSvSZ2Wdz6Px">
      <Paragraph id="4fEemuYAs9QOMDkHytdSrk">
        查看器+幻灯片+基本编辑 / HLS 转码 / 搜索 / 微信H5 / 移动端备份 / 360照片
      </Paragraph>
    </TableCell>

    <TableCell id="juiZDuCsXjPqit8QiKHdCd">
      <Paragraph id="j3lmVfh4TkjJDauUNSODvz">
        编辑可用、远程自适应
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="tR1426GLnO14NEOKil6U0W">
    <TableCell id="i2OzuUWQHsPIp4zXxTe0sP">
      <Paragraph id="skfoBME57xaOROaO1ry0BW">
        P2
      </Paragraph>
    </TableCell>

    <TableCell id="lyU46UyG6TH7nFEKKC4wFL">
      <Paragraph id="QpvVG2UdtK5nZgDC2aJNx9">
        多用户/SSO/2FA/审计 / 3D 地图 / 工具箱去重 / 回忆影片
      </Paragraph>
    </TableCell>

    <TableCell id="5EMjgfvtt6eQAXq0R5S8qD">
      <Paragraph id="kklO3ClqfmhA9wn7FFMrcY">
        企业级权限
      </Paragraph>
    </TableCell>
  </TableRow>
</Table>

<Divider id="4g0n13KoFqwJ5qbwZ1KH8g" />

<Heading id="h3J7T2ZZYvFAX3tRaW8Mt4" level="2">
  11. 风险与对策
</Heading>

<Table id="Uw4DgaUAuNrBLjRvaHP8Xq" readonly rowHeader>
  <TableRow id="rTOWJfYhktBkAsMJLTwvV2">
    <TableCell id="LBrlgvmv3oSOKVB9sCcr4J">
      <Paragraph id="pZ6M96EtKO7UT7CFi8ZGlW">
        风险
      </Paragraph>
    </TableCell>

    <TableCell id="UD7YQ26Uj0jUT1l8jArjpp">
      <Paragraph id="miMQYTP3wKw9x8eaTfqwyr">
        影响
      </Paragraph>
    </TableCell>

    <TableCell id="ummtzb81y0BNDE8OToC8TG">
      <Paragraph id="8UkPUQKjgAdrtMyuHUNeXF">
        对策
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="70hrO564u40csQQmHRubVo">
    <TableCell id="Lc8DvArB1sJ6owI4TlEk3C">
      <Paragraph id="r88QHi8FqM1fHzyqgVDvbv">
        无 GPU / 算力不足的主机，AI/转码慢
      </Paragraph>
    </TableCell>

    <TableCell id="2vLgxXoxrlJ2ZPMwHFKYEk">
      <Paragraph id="oFkRfJzuyEDAEGnE70Ph0K">
        卡死
      </Paragraph>
    </TableCell>

    <TableCell id="hAxmlffuSOLOSMPCrAEJ33">
      <Paragraph id="VP6XCOTXcG5C5jgTBae8gJ">
        存储与算力分离：存储节点仅存，算力节点跑 AI/转码；`EMBED_DEVICE=auto` 回落 CPU
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="AIG2UbGHG3kp1TMrzfR7va">
    <TableCell id="mmHRj3hcdzaGXWRtxIwLcW">
      <Paragraph id="78iqC47TFT95IX0oKFSrg8">
        微信强制 HTTPS
      </Paragraph>
    </TableCell>

    <TableCell id="j1FYC56CdLZd0liHrU3mst">
      <Paragraph id="WSfo1n4Xi2fdb59S8KDHSd">
        陀螺仪/WebXR 不可用
      </Paragraph>
    </TableCell>

    <TableCell id="nx9vINbAu2tgLMiYVCgOYd">
      <Paragraph id="LaBBTEV3vNjaGqrSjXk2bL">
        Caddy 自动证书 + 公网域名 + 反代
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="XPNtowXda9KP9Xua3LBNCP">
    <TableCell id="PzQZU0O5KagnsDSXsE34Oi">
      <Paragraph id="9zW4eZGP47WcIL4XpuKnjb">
        自研维护成本高
      </Paragraph>
    </TableCell>

    <TableCell id="TW33uaNF7V3eRI0VaVm1QH">
      <Paragraph id="IuaCEQDig6CYIDXVrVBA7o">
        长期负担
      </Paragraph>
    </TableCell>

    <TableCell id="azRVyG14yEqt8BSo2gxK8L">
      <Paragraph id="hqvWA4tMdAVpZiNI6v8Fwj">
        聚焦自研 360 引擎；其余用成熟库快速拼装
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="guhGfrpIve64Qun11VWu8S">
    <TableCell id="STsSepREZnmUlPRApYOlzZ">
      <Paragraph id="afEnkFiFEjG1nppFO51bbB">
        360 元数据缺失
      </Paragraph>
    </TableCell>

    <TableCell id="EsU3PHTvyMWEqgwNn7gm9p">
      <Paragraph id="UtL9eHMiw8V38bfaawLx4m">
        不被识别为球面
      </Paragraph>
    </TableCell>

    <TableCell id="cGmwO1WcuoHwzQwNgv4nBw">
      <Paragraph id="plBFGkK6GF6CX5vNF67fQl">
        导入补全 equirectangular 标记
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="Iawoid4XKX4dNovisiCPqj">
    <TableCell id="VHRZD3qy6QXPXgxmACOnBV">
      <Paragraph id="t0tlIVlioePsMWfS5eJ0EL">
        大库索引耗时
      </Paragraph>
    </TableCell>

    <TableCell id="ezpDOXU2wUD22GigseYl2E">
      <Paragraph id="90GYaoEeCdpgMzDlgdtoOM">
        迁移长
      </Paragraph>
    </TableCell>

    <TableCell id="RZ8Y5rAEB8VxRe4LjZgDLV">
      <Paragraph id="vSrwnaHx9mXtvs95GWrVyp">
        增量索引 + 低峰调度 + 进度可视化
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="z8RJyEa7Aj60Su9tc27z2G">
    <TableCell id="3xvvptbj9sJWLDzftunsor">
      <Paragraph id="SKgBDxYYqo4t7Q6DwmGxc0">
        SMB 持续写入与索引冲突
      </Paragraph>
    </TableCell>

    <TableCell id="rypSPLNZXQGXbuc5zmhVWu">
      <Paragraph id="86rYC06P7WgNMtlTdfXdcF">
        缩略图延迟
      </Paragraph>
    </TableCell>

    <TableCell id="y6uzoIttQi3ZjFzcfknagX">
      <Paragraph id="yVzkMjTMZVk5iRK3ndaBI0">
        文件监听去重 + 入库队列限速
      </Paragraph>
    </TableCell>
  </TableRow>
</Table>

<Heading id="P4VbQNOUb0pI1CNsJzriUP" level="3">
  11.2 数据迁移路线（从群晖 Photos 迁移 · 可选模块，非部署前置）
</Heading>

<Code id="auoVqWDD1GCu7Cnv8zyJZn">
  ```
  群晖 Synology Photos 导出（示例来源；非群晖环境可跳过本模块）
    → 扫描 <群晖默认媒体根，按实际修改>（如 /volume1/photo/）+ @eaDir/（群晖缩略图/sidecar）
    → 保留 EXIF + 原始目录结构（folder_path 映射）
    → 读取群晖 sidecar（.info, .extoolkit）提取人物/标签/收藏映射
    → 映射到本系统 media/people/tags/albums 表
    → 增量同步：rsync + inotify 监听新文件（迁移期间双系统并行）
    → 进度可视化 + 冲突处理（重复 pHash 去重 / 路径冲突策略）
  ```
</Code>

<BulletedList id="wYxWPBZQpKCBRMmObJK6Bv">
  迁移期间群晖 Photos 设为只读，本系统增量写入；迁移完成后切换流量。
</BulletedList>

<BulletedList id="4hsQdM3o1yzK15XrqQGLPJ">
  迁移工具详见 TDD §8.8。
</BulletedList>

<Heading id="Lu9P2L7YtEhzHLuybhwlDh" level="3">
  11.3 技术债务与合规风险
</Heading>

<Table id="e1eklUhwqNc1yjMp97Vt8k" readonly rowHeader>
  <TableRow id="MuQ0G3A3oZ5B8nfWMOxfH6">
    <TableCell id="clJAj16qPT3Nco2FZeQSpD">
      <Paragraph id="0ZHEkFP7dubryOeJIRm56V">
        风险
      </Paragraph>
    </TableCell>

    <TableCell id="0AoCK0HqjtBLqZDMP1oZjA">
      <Paragraph id="FmAKY5jSJu5iU56nV6pPMi">
        对策
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="ROMFRkGTDehKRLTpTWnFp8">
    <TableCell id="ct3lTCiRi6e4hxOKQC1sPr">
      <Paragraph id="tzY6BTm2cQcXHdOA3ZBDd0">
        insightface 模型许可（最终未采用——改用 OpenCV Zoo YuNet+SFace，Apache-2.0）
      </Paragraph>
    </TableCell>

    <TableCell id="5l7NrIO5MOUwzHmiDFdABs">
      <Paragraph id="rkklnRKQ9mgymfVxeBxPkv">
        P2 商业化前替换为自训练/MediaPipe/商用模型（见 §12.2）
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="XbpIOxXKpvhcVMQmG6st13">
    <TableCell id="ZzCx2QXdjfKBfwAZWuL6W9">
      <Paragraph id="6KTtHKBOOfPC7BWTJOeNVe">
        ffmpeg 静态链接 GPL
      </Paragraph>
    </TableCell>

    <TableCell id="2YK40lIrAddCTLTREgO9ES">
      <Paragraph id="464fy0ytQGWdkXLa2dq51z">
        架构评审确认 subprocess 调用模式（见 §12.2）
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="gtrd4cl2BQfsRgIs8Gc639">
    <TableCell id="EMzfegfFoJjbrZA3rnPVMV">
      <Paragraph id="eMBw91We0QBGF8vk0QaXWM">
        Redis RSALv2
      </Paragraph>
    </TableCell>

    <TableCell id="3oaCHVum7KCf8OiN9nU8lP">
      <Paragraph id="n2J1ca2mQmDd9MJRGDma6v">
        替换为 Valkey（见 §12.2）
      </Paragraph>
    </TableCell>
  </TableRow>
</Table>

<Divider id="8JaFeVvh0tJXO15eq9wBS4" />

<Heading id="MatttmFz8cla1VtSuDAkMd" level="2">
  12. 附录
</Heading>

<Heading id="3mdglw2UdkjTkQwDG01DpI" level="3">
  12.1 技术选型
</Heading>

<BulletedList id="xr85Csc8pSkid4R5u4QCCU">
  后端：Go 1.22+（API/媒体），Python 3.13（FastAPI AI）
</BulletedList>

<BulletedList id="E2q07ZSmn9GP35xuCuH86K">
  存储：对象存储（MinIO/S3 或 <Mark bold>SeaweedFS</Mark>）/ NAS 文件系统；PostgreSQL 16 + <Mark bold>PostGIS 3.4</Mark> + pgvector 0.7
</BulletedList>

<BulletedList id="rVgVdoXolBDN6qiEaQ8A4S">
  前端：Vue3 + Vite；MapLibre GL；A-Frame/Three.js；hls.js
</BulletedList>

<BulletedList id="A34mKu1khHNsRo5GOSTzQX">
  转码：ffmpeg 6（<Mark italic>subprocess 调用，不链接 libav 库</Mark>\*）+ NVENC；队列 BullMQ + <Mark bold>Valkey</Mark>（Redis BSD fork）
</BulletedList>

<BulletedList id="qQQtutqpWpweteENmXIiPt">
  部署：Docker Compose → K8s；Caddy 反代
</BulletedList>

<BulletedList id="XeJMPPAf1pnLzLRwvF6epJ">
  鉴权：Keycloak（OIDC）/ Authelia；otplib（2FA）
</BulletedList>

<BulletedList id="puYbBGcblJGK5DqI3nXiUq">
  监控：Prometheus + Grafana + Loki + Sentry/GlitchTip
</BulletedList>

<Heading id="FYyq7A1rKWtKtWpZcTGILD" level="3">
  12.2 第三方依赖许可证商业化合规矩阵
</Heading>

<BlockQuote id="h5LfAaC87VrC9lXGD25vPu">
  <Paragraph id="1DoBPyRQaB7TfIc6TUmZGD">
    <Mark bold>目标</Mark>：确保所有技术路径不阻碍未来产品商业化。本矩阵由竞品分析师（竞析）审计确认。
  </Paragraph>
</BlockQuote>

<Paragraph id="RkW4uw5tVIjKo4LVhYpmgv">
  <Mark bold>A. 确认安全（MIT/BSD/Apache/PostgreSQL 许可，无商业化影响）——直接使用</Mark>
</Paragraph>

<Table id="naetVZgtA8S9N3mpw0Dqk4" readonly rowHeader>
  <TableRow id="Yx3dc772wHrbi6UFPswJ0S">
    <TableCell id="CYOW8osAtJReWKQrS7nIkl">
      <Paragraph id="889j9W2PixWUMh6tDbzifC">
        库/服务
      </Paragraph>
    </TableCell>

    <TableCell id="x8azZhxPLj5fzPfZBaLU47">
      <Paragraph id="nzXk2qfLKCh8xw6M04wfRv">
        许可证
      </Paragraph>
    </TableCell>

    <TableCell id="eE9RYebOzB19O3UTQsy9VB">
      <Paragraph id="adBrsppe6C6zh4of8KrVS1">
        用途
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="IQSoKF2AKzXjM8UPVxXX8O">
    <TableCell id="me4lp7G23ELu3cT9M3klv8">
      <Paragraph id="4iRPrJ4BgGIS43pd4j7aga">
        Three.js
      </Paragraph>
    </TableCell>

    <TableCell id="kZoB5Ayg5i9EJqqOx3tL4A">
      <Paragraph id="dCMvcRuhcRCSSCO86tdfzD">
        MIT
      </Paragraph>
    </TableCell>

    <TableCell id="ojVGA5hnGEMUj4Ek6iJcKA">
      <Paragraph id="60CFDXtHhT5o6pB5zL9RWh">
        360 球面渲染
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="4N5UDFcVwg4wXCuuS5E9tc">
    <TableCell id="JcxBmH3OuSHQf8cqImSEe8">
      <Paragraph id="1VRwHZdVAuuG9hgAiWEJre">
        A-Frame
      </Paragraph>
    </TableCell>

    <TableCell id="tDO3VxFxEgKH0JTS8lOTGl">
      <Paragraph id="jhwmuyPmYwvfY2MBUED4rs">
        MIT
      </Paragraph>
    </TableCell>

    <TableCell id="fe46HFCRa0xoNeCFOBaOj7">
      <Paragraph id="m3lBGWhzxaRWsORF3hoHOj">
        VR 场景
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="yu82f6E1TFRKL6zTqsHMc6">
    <TableCell id="6GsShJbvshIpciNvICjA2P">
      <Paragraph id="yL6OrmzYsawm6qqNkUBmeY">
        MapLibre GL JS
      </Paragraph>
    </TableCell>

    <TableCell id="58sB0Ft8FJ6vHrAm6dDXzZ">
      <Paragraph id="6yAqICbeVvPiXpDApWg8Sf">
        BSD-3-Clause
      </Paragraph>
    </TableCell>

    <TableCell id="bMD9DeFIfxXhGGnIFegReU">
      <Paragraph id="HcsMZgDn9BIiIgSG3LJxoi">
        地图渲染
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="rOTtTkhjIxXoYaq0fJZtHT">
    <TableCell id="DrUVjauhNBBidZF8JTmsla">
      <Paragraph id="TI7YgF61Hl3LLI7V9pJUCH">
        hls.js
      </Paragraph>
    </TableCell>

    <TableCell id="tB9d0MXm0v957OeQidX8pT">
      <Paragraph id="yx3pS8wK5FIvu9CklurT6h">
        Apache 2.0
      </Paragraph>
    </TableCell>

    <TableCell id="X2EsYoi8ycWCQllO3jmdux">
      <Paragraph id="WMMk5kryXK9d0L6Po6T792">
        HLS 自适应码率
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="bGnUg9DpkTm8sNkvZmxU2c">
    <TableCell id="8oF1c5LKUbjFRptW3c6ZDA">
      <Paragraph id="plmFfTOJb569JFXqJlRV9N">
        Vue 3 / Vite
      </Paragraph>
    </TableCell>

    <TableCell id="HrmXxPCX2kgddrX7PT3JlQ">
      <Paragraph id="XnxaDcjU8fX96cDsHkl2Oe">
        MIT
      </Paragraph>
    </TableCell>

    <TableCell id="IdaS7iMmCmfHWzkewYHwJ8">
      <Paragraph id="tc9IMgliXvCW7A6oaBFd7f">
        前端框架
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="XikjwkqVMsMXPqdJ0KwXku">
    <TableCell id="rvCnARbn7AYHjFu7Ikq8wp">
      <Paragraph id="uEpauJ1Oi1k5xGdQaLli0L">
        Caddy
      </Paragraph>
    </TableCell>

    <TableCell id="7iBVtukWXh05JoEzISDAVI">
      <Paragraph id="zJZczhsmqZtknFrh2qqq6i">
        Apache 2.0
      </Paragraph>
    </TableCell>

    <TableCell id="5QnIhMozsrg4VYVRvS8EvN">
      <Paragraph id="VT9gfdPYFh4rLWPzZ8gnyU">
        反代+自动 HTTPS
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="JTB9frHpQ8q2SW1A28fxE4">
    <TableCell id="0U4Cao52H3Dx215AuWecLN">
      <Paragraph id="PKgoe7epCep8kQ6DvgK2rb">
        Keycloak
      </Paragraph>
    </TableCell>

    <TableCell id="Jhvq8IZ3XjSWBj5ncUYPOA">
      <Paragraph id="iX8mbKJ0CLmWZavMaHPcMN">
        Apache 2.0
      </Paragraph>
    </TableCell>

    <TableCell id="H2CxkJFWnY2M6bIHvYZuXS">
      <Paragraph id="rGSGFfX74XK0KT6A7cxWZA">
        SSO/OIDC
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="dHCYWqkGRTDlm86MPui3xr">
    <TableCell id="z5q1AsWWlUeYJP6MB7geZk">
      <Paragraph id="zuQKSbBVbKKGu5y0Qal5mm">
        Authelia
      </Paragraph>
    </TableCell>

    <TableCell id="XUaAbjG6obtKoIm60bokb9">
      <Paragraph id="Vj4lXV1PEMkJuS4QKbvwZU">
        Apache 2.0
      </Paragraph>
    </TableCell>

    <TableCell id="o9tOWwAY24vkzi388Y36Sn">
      <Paragraph id="zUoCNzJZAx5QWlUx9vvVja">
        替代 Keycloak
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="wFnnLwAvLwJrPawJsVSSNz">
    <TableCell id="pACLvoDbmo2hbAbf6zeBB9">
      <Paragraph id="BrnoCNWs8taEbIBopYiU8f">
        otplib
      </Paragraph>
    </TableCell>

    <TableCell id="ctzOm07H7MptH1ME2107GL">
      <Paragraph id="Xxfyd9m3ePO8LHOWNWQzBS">
        MIT
      </Paragraph>
    </TableCell>

    <TableCell id="Hxa0iptX5a1ryyJfBBF6FY">
      <Paragraph id="tValrbWo9dLUOfT9JTh9RJ">
        TOTP 2FA
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="SfSiKefLts0pwSszfMTSDg">
    <TableCell id="Dr35gOCC474y2v0HkQHCI9">
      <Paragraph id="Nkhfmyd5evcrNwdkZ34Mfb">
        exifread
      </Paragraph>
    </TableCell>

    <TableCell id="7ry2d0TYjOc5GqPfPIfbiQ">
      <Paragraph id="0dToYziSzJm7UYoNPMvMHk">
        MIT/BSD
      </Paragraph>
    </TableCell>

    <TableCell id="4GRYXQ4a1sMLxTIYUv0Q5a">
      <Paragraph id="vI2OQQY19y9xaubMqZc5JC">
        EXIF 提取
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="bSdH7ZJNPyFdceqzbYPabb">
    <TableCell id="lTfKBDS6HZ7izabnmt3pJ2">
      <Paragraph id="gENorf7mlZM5tuT9YohkWl">
        supercluster
      </Paragraph>
    </TableCell>

    <TableCell id="12GLO23XNVZ8APG0IbW6TJ">
      <Paragraph id="9e70qTZn0f5u6EJpKibrJO">
        MIT
      </Paragraph>
    </TableCell>

    <TableCell id="kUPqRwBkjZ2NuqZaiwoBnK">
      <Paragraph id="yGN9WVh8Yo2c5yurUPma6C">
        地图聚合
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="UN61QURWL01VVjkcRH7ZNb">
    <TableCell id="eZD1CY3cpYhX9Af0dtKRfL">
      <Paragraph id="8RtHdgiEmvHNzxf8cjg2SD">
        Planetiler
      </Paragraph>
    </TableCell>

    <TableCell id="TPN1tRgTWAPNnId0Mv24zU">
      <Paragraph id="1dNLgPwoLTPtW8DZwxF3n2">
        Apache 2.0
      </Paragraph>
    </TableCell>

    <TableCell id="kIF1u901Rp9dDESUL4nr29">
      <Paragraph id="qdhumE9phK3gWbSj4XnNWo">
        矢量瓦片生成
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="V6rbVPgL9j7Qfaue3WkERC">
    <TableCell id="PAZyflqOyyBmTq7gvl7OUi">
      <Paragraph id="SeyrMHU5HSGPrKSb2ZMIcw">
        pgvector
      </Paragraph>
    </TableCell>

    <TableCell id="JCKD4o4rodfIhilp18vG2b">
      <Paragraph id="pK8laSbrKovjAvjTuRCVZc">
        PostgreSQL License
      </Paragraph>
    </TableCell>

    <TableCell id="h2dCq3S7tL4DUl56FQcJMf">
      <Paragraph id="2BlcbIt5WtSGHPsHgd99MT">
        向量检索
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="CRWmDomwCfvRWt8P8W0r96">
    <TableCell id="Tkj6SO5uc2kCtjv3N9q6Nd">
      <Paragraph id="pKEbAvQ0CYVPuMzvLHorQt">
        PostgreSQL
      </Paragraph>
    </TableCell>

    <TableCell id="Mmhbp6lkKo0uFV7lZ3aQ5C">
      <Paragraph id="yGJrWv2bYs8qIIszKCY5Te">
        PostgreSQL License
      </Paragraph>
    </TableCell>

    <TableCell id="JzsANU4wJnp3FNKHWxnWUl">
      <Paragraph id="7x4kYXQe5RJItObNrKFIzm">
        主数据库
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="iZkKiPX93w085nLNGvGp73">
    <TableCell id="eaHXVEW8P5PdnPj4M8SzUJ">
      <Paragraph id="2gwJl0ixgRS364BP05e1FU">
        FastAPI
      </Paragraph>
    </TableCell>

    <TableCell id="AhQDeFEys3SrcUzkE2g9sJ">
      <Paragraph id="DuOlu5LybQVjFHZ04ncFeZ">
        MIT
      </Paragraph>
    </TableCell>

    <TableCell id="XAJlG5CZXbtnlC3NVEpR6v">
      <Paragraph id="ct2duFrK9WWulmVydkNusz">
        AI 服务框架
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="E9ZdEcox41ydZ6r4Tpdqfu">
    <TableCell id="Ozq6hHkHjHEbgZVQAWKdFY">
      <Paragraph id="hnaHhPajcXZe6CdDGgmXFG">
        Go 标准库
      </Paragraph>
    </TableCell>

    <TableCell id="Z8Rxx8219SSwxNFjXlWT4r">
      <Paragraph id="OjfzNOb9X3Oih054c7qWVq">
        BSD-3-Clause
      </Paragraph>
    </TableCell>

    <TableCell id="VFietwv8oQslseZ02zMI9F">
      <Paragraph id="a8nrOclIM4ywBCC7p7qalB">
        后端语言
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="Ow6C2vlIPXvqFBhKhRCbDx">
    <TableCell id="XQA4vIY47tv6pRXvF5kDxN">
      <Paragraph id="LAlTAc1qCKsitTvLb9Bc3G">
        Python 标准库
      </Paragraph>
    </TableCell>

    <TableCell id="vlg5yolbYk27l3IvBrYul4">
      <Paragraph id="76pSCNHeJgN8rJWkD1gh4i">
        PSF License
      </Paragraph>
    </TableCell>

    <TableCell id="b0EH4fMIs2lSY8aVkcJ5WO">
      <Paragraph id="3sgIJjmRVH8V6SYM4eqhQ9">
        AI 语言
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="2ZjDq5uVrvTufcKXrUbbJQ">
    <TableCell id="enJnAxvzTqv7YYTu96d5aF">
      <Paragraph id="OpBMR2rAmwCD6vfjwBrLFB">
        libvips
      </Paragraph>
    </TableCell>

    <TableCell id="hMN8p1thmTssnHPYpcV9Mz">
      <Paragraph id="ggCiAUDexQ0e4Sgg7JOsDD">
        LGPL v2+（动态链接）
      </Paragraph>
    </TableCell>

    <TableCell id="QkBv2SQLEinzvplAXetKGL">
      <Paragraph id="KhqCrJNQpRvK4NjjOvSJNR">
        非破坏编辑
      </Paragraph>
    </TableCell>
  </TableRow>
</Table>

<Paragraph id="cd3vzRevewf9TNdrTxRRxO">
  <Mark bold>B. 需注意——有条件商业化安全</Mark>
</Paragraph>

<Table id="zIYFbcyC0dDF9AGBMVDiZ8" readonly rowHeader>
  <TableRow id="f24qv59pjRC2kcwMB1CmF3">
    <TableCell id="A3C6O3oeDpZoSebYMhy76P">
      <Paragraph id="f8yJRUeCydUwVt3K1hsVZe">
        库/服务
      </Paragraph>
    </TableCell>

    <TableCell id="zhCYUeeT9gS2E1FC2YqWLZ">
      <Paragraph id="vLNZBlV7vRzZcaI8icAa8u">
        许可证
      </Paragraph>
    </TableCell>

    <TableCell id="B9tvpxdd4pG23TdbX65Ier">
      <Paragraph id="aFpUcz4CaYzgPLwwpf3wZE">
        风险
      </Paragraph>
    </TableCell>

    <TableCell id="CTg0G1nKERVDsqt79idSRv">
      <Paragraph id="BmT89Ly2AtEQpIvI1qAExB">
        合规路径
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="vnJHvoUwjzl88MUPyokii3">
    <TableCell id="Y4gRc7gO2mEoxrGVa7Mnzw">
      <Paragraph id="UG8jL0mVdJeW4tbACp6RT1">
        <Mark bold>ffmpeg</Mark>
      </Paragraph>
    </TableCell>

    <TableCell id="9MeaVT1CLpVchUKxhMBvbi">
      <Paragraph id="YbQEBnjP0HzYllzuVpvzvx">
        LGPL v2.1 / GPL v2+（双授权）
      </Paragraph>
    </TableCell>

    <TableCell id="mMxQyQoVf56dHrQfaAvQSs">
      <Paragraph id="kwDPLvRzcSmj0GtSYFQIg3">
        静态链接 GPL 组件 → 传染
      </Paragraph>
    </TableCell>

    <TableCell id="TA9sod5ImuXCNNq9jgDIR6">
      <Paragraph id="Wk15F9lFKqS2QNQllLzs11">
        <Mark italic>必须以 subprocess 调用二进制（不链接 libav 库）</Mark>\*；H.264 编码用 NVENC（NVIDIA 免授权）或购买商业编码器；避免 `--enable-gpl` 编译选项
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="1ylN71a93gTFykGMf5lqUa">
    <TableCell id="y5P8B1iEnSMhcucg8S5rV4">
      <Paragraph id="SaGmcT7kQ2Rbq9hbGxtx7z">
        <Mark bold>insightface（最终未采用）</Mark>
      </Paragraph>
    </TableCell>

    <TableCell id="vgDpt1dSnf2J5W3kQ3OABN">
      <Paragraph id="WHrXO2bK6psf4QLg3FDnpT">
        软件 MIT；预训练模型各异
      </Paragraph>
    </TableCell>

    <TableCell id="4gCF92zGR9dldfJdQNxw7A">
      <Paragraph id="c2KNSMkNB2ElaJSR2OKOa8">
        buffalo\_l 等模型可能限非商用
      </Paragraph>
    </TableCell>

    <TableCell id="ey9kvuwJnePgV7zJqkYMAD">
      <Paragraph id="rnIjYgoqXEzxgAjB9X3PKW">
        <Mark bold>商业化前必须</Mark>：①自训练模型 ②购买商用许可模型 ③替换为 MediaPipe(Apache 2.0) + 自训练分类器
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="unQi63w2wNlJT8aeY69XMC">
    <TableCell id="SHulhCAjuglfEuSE35Q1br">
      <Paragraph id="kk6z6KzyDiuvqXRiCOPEBQ">
        <Mark bold>MinIO</Mark>
      </Paragraph>
    </TableCell>

    <TableCell id="7PBGZrVbxDikV2a1bfkEia">
      <Paragraph id="1a8h9456oNrSbgLZSrz73j">
        AGPL v3
      </Paragraph>
    </TableCell>

    <TableCell id="OS0FUTBIXprvq4yD1tE6SD">
      <Paragraph id="0IviPXRUCRQFtyBNljC9LD">
        修改后对外服务需开源
      </Paragraph>
    </TableCell>

    <TableCell id="ebMomcNjPWnoqxZeKKCfel">
      <Paragraph id="L4mmiwjokOFCH57Op9ePwa">
        当前用法（S3 API 调用，不修改/不分发）<Mark bold>不构成传染</Mark>；若需完全规避 → 替换为 <Mark bold>SeaweedFS</Mark>（Apache 2.0）
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="zNCqsZAtwMfHMtd4lhquAL">
    <TableCell id="kNpC1gUC6dNaacLprO8iCh">
      <Paragraph id="9mcQsmoIj4JgHtxIRYmsJ9">
        <Mark bold>Nominatim + OSM 数据</Mark>
      </Paragraph>
    </TableCell>

    <TableCell id="HR54HiPQcwvWoDwC4H2wpb">
      <Paragraph id="q8AZRDvcoke1e7IP6CPGQo">
        软件 GPL v3 / 数据 ODbL
      </Paragraph>
    </TableCell>

    <TableCell id="W8i9E3LwF7aKDOIxfgwcnW">
      <Paragraph id="CNa0mhuirM7xS6GNnNWEAo">
        ODbL 有分享条款
      </Paragraph>
    </TableCell>

    <TableCell id="R1ql0xiylBbORXgla13x8Q">
      <Paragraph id="xeSYcMtbypf3SW9ADYMlqZ">
        自建 Nominatim 需遵守 ODbL（署名 + 衍生数据同许可）；或用商业地理编码 API（如 Mapbox、Esri）
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="2zp0itdyYzjefSITB1MDT6">
    <TableCell id="tqnUgXq7RlDAMYC7qCgyrg">
      <Paragraph id="LOgSxoeBijutSYAiDKcBXB">
        <Mark bold>高德 Amap API</Mark>
      </Paragraph>
    </TableCell>

    <TableCell id="gPw25DyK8DB8SGZiOOBd2R">
      <Paragraph id="Am9ek1DXgzyMAg3Oc52KGs">
        商业 API 条款
      </Paragraph>
    </TableCell>

    <TableCell id="XkYSiDmrP079N401iVV2TF">
      <Paragraph id="Nlh8tCV2J3WFGkbdPiQwdq">
        免费额度有限，商用可能需付费
      </Paragraph>
    </TableCell>

    <TableCell id="fILsn5EcERzLdgijKGGtlI">
      <Paragraph id="383w46HazYEzPiiJpex4H1">
        需注册开发者 key；商业化前确认 API 使用条款与费用；key 后台加密存储
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="p3e8b1ku1vFkueTKSEraA3">
    <TableCell id="hjQIn7sKMd8X8ieQBZcfuj">
      <Paragraph id="Rth8nMWxUq5NKQ4MUBN1aA">
        <Mark bold>Maptiler</Mark>
      </Paragraph>
    </TableCell>

    <TableCell id="2wIBIMTlVky12C1U1xlboW">
      <Paragraph id="rddhgUur3M8SLC3WDkTGwR">
        商业服务
      </Paragraph>
    </TableCell>

    <TableCell id="EcwGQ0p8TWnG3lYDETwVAB">
      <Paragraph id="g6RHUetdPYn4MJqqRbOcP2">
        按量计费
      </Paragraph>
    </TableCell>

    <TableCell id="xTyiVk1Kz6SD30M8MMB0u1">
      <Paragraph id="qr4SNiJcDPCz7wsrWmIgmn">
        付费瓦片服务，需评估用量与成本
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="xRe8e7I8afI1jSTO8Y10uZ">
    <TableCell id="7F86moGcTkiNkKSRcs5Qz4">
      <Paragraph id="U4fUWGdDGWXzHgcYLiXrU5">
        <Mark bold>libvips</Mark>
      </Paragraph>
    </TableCell>

    <TableCell id="RjIQmNmKqus88B9ClVQDOB">
      <Paragraph id="9XfSesNUyWpX0Hkl7czIbQ">
        LGPL v2+
      </Paragraph>
    </TableCell>

    <TableCell id="qQFgSZ4X5djfBmEnqjQ3ls">
      <Paragraph id="8iPLGk2N8Affo8vDnpBO1F">
        静态链接需开源链接库修改
      </Paragraph>
    </TableCell>

    <TableCell id="CqpOtR7jj23TUmzc2sxU0o">
      <Paragraph id="fY40TCtON3gBNrVecwcYZv">
        <Mark bold>动态链接 OK</Mark>；如需静态打包 → 使用预编译共享库或单独协议授权
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="FgPJOcqWraHSmEGJkshPvk">
    <TableCell id="kHVBnwV8ukMpnY6VVjDhNn">
      <Paragraph id="KC5DSYPyPdFZE7JuO3kNQd">
        <Mark bold>PostGIS</Mark>
      </Paragraph>
    </TableCell>

    <TableCell id="3OO4lNte6sSux6PRwbS0Nu">
      <Paragraph id="TEXbvC1SRIKfcgsaz2he8g">
        GPL v2+
      </Paragraph>
    </TableCell>

    <TableCell id="FAm7hc7xEZ7lySWKVc3hKj">
      <Paragraph id="vK8iLBXFqYf9zYjWAw7ICQ">
        作为 PG 扩展在服务端运行
      </Paragraph>
    </TableCell>

    <TableCell id="MTPA4oSvsGMvTc6RuVbBCW">
      <Paragraph id="SNFCKZy84crMz2lSugpPjO">
        <Mark bold>自托管不分发即无传染</Mark>；与 PostgreSQL 一起在服务端运行安全
      </Paragraph>
    </TableCell>
  </TableRow>
</Table>

<Paragraph id="tm4cCZFkZbmOGjtfLI76ne">
  <Mark bold>C. 必须替换——许可证不兼容商业化</Mark>
</Paragraph>

<Table id="oZFx1ACxgi7qNZJ1ljoFOL" readonly rowHeader>
  <TableRow id="AxWu8beFwvlQg7CbA7BE9Z">
    <TableCell id="yKKzwok32QL4VAewCIJOLk">
      <Paragraph id="K4qFF3wYkNW6yFXTCwaDz6">
        原方案
      </Paragraph>
    </TableCell>

    <TableCell id="sqjugcGxZT42GKpvnSaftG">
      <Paragraph id="ljCLiG8W65ed5qkQ6AUFT6">
        问题
      </Paragraph>
    </TableCell>

    <TableCell id="12skDDx0vox08jaRTzLb5G">
      <Paragraph id="d9fY8c7eC9p3qq0959Znk1">
        替换方案
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="AFLcPGSH2VnUz57DvWAPge">
    <TableCell id="Xp1kPE9TaYG4eiYOXGaRCv">
      <Paragraph id="6oQvSNXlIL0363KUjZ9Gl7">
        <Mark bold>Redis</Mark>
      </Paragraph>
    </TableCell>

    <TableCell id="xQconBvvRrpOtba4lIZFOu">
      <Paragraph id="WB1udzitxVNfN7RpvxDa4o">
        2024 起改 RSALv2/SSPL（非 OSI 认证开源），商用需商业授权
      </Paragraph>
    </TableCell>

    <TableCell id="arNCxogXqyMC7gJ3rbRE3Q">
      <Paragraph id="w8BzWxZKVWH9mS3PweJXSK">
        <Mark bold>→ Valkey</Mark>（Linux 基金会 BSD-3-Clause fork，BullMQ 完全兼容）
      </Paragraph>
    </TableCell>
  </TableRow>
</Table>

<Heading id="obiJzjOeEeSZGx6GgaLoaI" level="3">
  12.2.1 商业化合规行动清单
</Heading>

<Table id="2RSuHe0EUEk4XQkCd85AFA" readonly rowHeader>
  <TableRow id="ecRxjDKdEYtSEfHGvgemxe">
    <TableCell id="Ax2V1MZzs1qQ4Op3che4lb">
      <Paragraph id="LgcGfmDLUluo5dgH3gTWmm">
        优先级
      </Paragraph>
    </TableCell>

    <TableCell id="dzAxhRrkoRAoGDyt18POi7">
      <Paragraph id="DvzZNMQsBEuUBXcCd3KiC8">
        行动
      </Paragraph>
    </TableCell>

    <TableCell id="gsz0lkZSvbeZfsymJQUPgv">
      <Paragraph id="kvY26g78ZwYXtXXkBsz2aj">
        阶段
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="6ATK2Fy0nCuvgOjeCP9aNj">
    <TableCell id="TGSLTcQzFEOjZm8rT47RwP">
      <Paragraph id="U22SRb0vFKtaSEe3ea3RsP">
        P0
      </Paragraph>
    </TableCell>

    <TableCell id="DIZc1gt1eS3PHeiesrLuek">
      <Paragraph id="bgIrPTUayEPibBFJC35PY1">
        Redis → Valkey 替换（BullMQ 配置改 `redis://` → `valkey://` 即可）
      </Paragraph>
    </TableCell>

    <TableCell id="9ZF5XliBg1pjZN7CfOH1dX">
      <Paragraph id="WJ2Qlu1SiOR2rWolz9MIYs">
        开发前
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="PlWFjZzQtUvu4F34JkFTH9">
    <TableCell id="43uGBmCayvd6AWHsS7SVlQ">
      <Paragraph id="dLsPOUdVV5qhCtGo2UyVEP">
        P0
      </Paragraph>
    </TableCell>

    <TableCell id="wMrqlCpEQh3A8wHlyxqI9f">
      <Paragraph id="FIUIZdOoSPDhFD2adVz7nr">
        ffmpeg 确认 subprocess 调用模式，禁止链接 libav\*
      </Paragraph>
    </TableCell>

    <TableCell id="4rRMF2ucWXFEAZp2Wvjxlg">
      <Paragraph id="zyxSWAwKiKXrRRUSdSZXn0">
        架构评审
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="FxNQwGvYlW3BBVrRhDSm1L">
    <TableCell id="827tJFF7N5inc1SAh8qplj">
      <Paragraph id="o2y3CqVwX6SQhoAZKOehjY">
        P1
      </Paragraph>
    </TableCell>

    <TableCell id="VXgAwSG6t7i1CUjrGXrVyj">
      <Paragraph id="BHTwMUUuE8OltPbFYBmga0">
        insightface 模型许可评估：自训练 / MediaPipe 替代 / 商用购买 ——【已闭环】最终改用 OpenCV Zoo YuNet+SFace（Apache-2.0），无需自训练或商用购买
      </Paragraph>
    </TableCell>

    <TableCell id="7YVfO4lDnnVCY3F5M60F9W">
      <Paragraph id="9Hq3AUatwsArCRsHQPlxST">
        P2 商业化前
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="7uUsaX3DAsU9E3V32uP6cq">
    <TableCell id="YFtrgpsfWE8xFkSEDjYTul">
      <Paragraph id="JEKMpGn1sXKEhySTYdzVjD">
        P1
      </Paragraph>
    </TableCell>

    <TableCell id="ayvVIw2E1XjsRkJEbEA7lP">
      <Paragraph id="0xraq7f8SfbsKI9LYCC1KF">
        MinIO 评估：保留 S3 API 调用模式 或 迁移 SeaweedFS
      </Paragraph>
    </TableCell>

    <TableCell id="qOGzRxDOHe9buSiEacwOFI">
      <Paragraph id="NPiZIhcgSJp9VppaVYdXXj">
        部署时
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="YNK3T74TDjW8scxuBM75I1">
    <TableCell id="eY5OJr3YLFIPqjG9c09PlS">
      <Paragraph id="FFS2vkcNC8ggRJ04H5v47E">
        P2
      </Paragraph>
    </TableCell>

    <TableCell id="TymAkFHwO860coVi6jkqbl">
      <Paragraph id="JtgzcGjVXojYwxiLd7rOpK">
        Nominatim/OSM ODbL 合规审查 或 迁移商业地理编码
      </Paragraph>
    </TableCell>

    <TableCell id="3N4swKFGlntmDzoCu9jBsP">
      <Paragraph id="sBMIHdMmefxpWP8BQ31ZMj">
        P2 商业化前
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="aWi7OZvnXcLV50xw7Ypdey">
    <TableCell id="e2vEJwZcZj6sjBhkUIQW22">
      <Paragraph id="jGaK8Cdg2cdrZ4RRezS6mu">
        P2
      </Paragraph>
    </TableCell>

    <TableCell id="GKLieD3y0mglz2hhrpj6Hx">
      <Paragraph id="M8dtL3Zb01coprDAdWJenx">
        高德 API 商用条款审查与费用评估
      </Paragraph>
    </TableCell>

    <TableCell id="LvUXDEO0zj4khiykEpzcPz">
      <Paragraph id="hyCMpe8YkvNb2gR8LUAKZ8">
        P2 商业化前
      </Paragraph>
    </TableCell>
  </TableRow>
</Table>

<Heading id="qQV5BEvC4g7gAHViUNN8oj" level="3">
  12.3 开放问题（已全部确认）
</Heading>

<BlockQuote id="hkyHWybPZURQ5iQCbBtsPB">
  <Paragraph id="acxfoFsCbhsy59iUquFZRC">
    以下四项已于第二轮确认，决策注记见本文「第二轮开放问题决策（已确认）」块（§1.4 对照表之后）。此处仅作状态归档。
  </Paragraph>
</BlockQuote>

<BulletedList id="jy9sUH271rBY42QDbcgDTr">
  <Mark strike>GPU 算力节点形态（本地 mini PC / 云 GPU / 复用 360VideoConverter 机器）？</Mark> → <Mark bold>已确认：多形态（本地 GPU / 云 GPU / 本地网络第三方 GPU 主机 Win·Linux），经算力节点抽象层 + 调度器统一接入。</Mark>
</BulletedList>

<BulletedList id="hHFnEQUqieiL8wjFzymxze">
  <Mark strike>微信分享接受 H5 链接，还是需要小程序级体验？</Mark> → <Mark bold>已确认：H5 链接优先实现，小程序级体验为第二阶段。</Mark>
</BulletedList>

<BulletedList id="vvLXxMNbjHR5JpPRyJYy7t">
  <Mark strike>是否需「共享空间」多人实时互见，还是以共享相册为主？</Mark> → <Mark bold>已确认：以共享相册为主，不要求实时互见。</Mark>
</BulletedList>

<BulletedList id="SUuNjqPwrfMSSOc6ci4sYN">
  <Mark strike>公网域名与带宽预算？</Mark> → <Mark bold>已确认：支持自定义域名 + IP 直连 + HTTPS（含非标端口/路由器端口转发）；带宽支持手动指定与自测，并据此推荐实时码流率。</Mark>
</BulletedList>

<BulletedList id="0bgMC2exp2q1iwWPCkzcAr">
  <Mark strike>是否保留与 DSM 账户体系对接</Mark> → 已确认：采用独立后台账户，不对接 DSM。
</BulletedList>

<Divider id="zZIh5eRjZpiIjlrd8aie4z" />

<Paragraph id="U0prRJHnJ8HMGhdrSO8kog">
  <Mark italic>文档结束（v3.1）。下一步可进入《技术设计文档》《数据库 DDL》《API 详细契约》或《360 播放引擎原型（Docker Compose + 反代 + HLS 转码脚本）》。</Mark>
</Paragraph>
