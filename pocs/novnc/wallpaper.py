from PIL import Image, ImageDraw, ImageFont
im=Image.new('RGB',(1280,720),'#101b32')
d=ImageDraw.Draw(im)
for x,y,r,c in [(1140,50,350,'#142441'),(50,700,300,'#12213c')]:
    d.ellipse((x-r,y-r,x+r,y+r),fill=c)
font='/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf'
d.text((100,240),'✦',fill='#70ddb9',font=ImageFont.truetype(font,80))
d.text((195,260),'Sidekicks',fill='#e3edff',font=ImageFont.truetype(font,64))
d.text((200,350),'COMPUTER USE / DISPOSABLE DESKTOP',fill='#93b0da',font=ImageFont.truetype(font,18))
d.text((200,390),'One owner. Many collaborators.',fill='#93b0da',font=ImageFont.truetype(font,18))
im.save('/tmp/sidekicks-wallpaper.png')
