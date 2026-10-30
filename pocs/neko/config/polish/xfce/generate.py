#!/usr/bin/env python3
"""Render canonical Sidekicks Xfce config into an image-build staging directory.

No commands are executed and no live user directory is read or modified.
Backend owners install user/ into the guest home and system/ into /etc/xdg.
"""
import argparse
import copy
from pathlib import Path
import shutil
import xml.etree.ElementTree as E

ROOT=Path(__file__).parent

def property(parent,name,kind,value=None):
    attrs={'name':name,'type':kind}
    if value is not None: attrs['value']=str(value)
    return E.SubElement(parent,'property',attrs)

def write_xml(path,root):
    E.indent(root)
    E.ElementTree(root).write(path,encoding='utf-8',xml_declaration=True)

def render(output,home,commands,wallpaper,icons,monitor='screen',terminal_class='SidekicksTerminal'):
    for value in [home,wallpaper,icons,*commands.values()]:
        if any(ord(c)<32 for c in value):raise ValueError('configuration values cannot contain control characters')
    if not monitor or '/' in monitor or any(ord(c)<32 for c in monitor):raise ValueError('invalid XRandR monitor name')
    out=Path(output)
    user=out/'user';system=out/'system'
    channels=user/'.config/xfce4/xfconf/xfce-perchannel-xml'
    system_channels=system/'xfce4/xfconf/xfce-perchannel-xml'
    channels.mkdir(parents=True,exist_ok=True);system_channels.mkdir(parents=True,exist_ok=True)
    for name in ['xsettings','xfwm4','xfce4-session','xfce4-desktop']:
        tree=E.parse(ROOT/'templates'/f'{name}.xml')
        if name=='xfce4-desktop':
            screen=tree.find("./property[@name='backdrop']/property[@name='screen0']")
            prototype=copy.deepcopy(screen[0])
            for child in list(screen):screen.remove(child)
            for monitor_key in ['monitor0','monitor'+monitor]:
                child=copy.deepcopy(prototype);child.set('name',monitor_key);screen.append(child)
            for p in tree.iter('property'):
                if p.get('name')=='last-image':p.set('value',wallpaper)
        write_xml(channels/f'{name}.xml',tree.getroot())
    shutil.copyfile(ROOT/'templates/xfce4-panel.xml',system_channels/'xfce4-panel.xml')
    terminal=user/'.config/xfce4/terminal';terminal.mkdir(parents=True,exist_ok=True)
    shutil.copyfile(ROOT/'templates/terminalrc',terminal/'terminalrc')
    menus=user/'.config/menus';menus.mkdir(parents=True,exist_ok=True)
    shutil.copyfile(ROOT/'templates/xfce-applications.menu',menus/'xfce-applications.menu')
    apps=user/'.local/share/applications';apps.mkdir(parents=True,exist_ok=True)
    for index,app in enumerate(['browser','terminal','files'],1):
        text='\n'.join(['[Desktop Entry]','Type=Application','Name='+app.title(),
            'Comment=Open or restore the Sidekicks '+app,'Exec='+commands[app],
            'Icon='+str(Path(icons)/(app+'.png')),'Terminal=false',
            'StartupWMClass='+{'browser':'SidekicksBrowser','terminal':terminal_class,'files':'Thunar'}[app],''])
        name='sidekicks-'+app+'.desktop'
        (apps/name).write_text(text)
        panel=user/f'.config/xfce4/panel/launcher-{index}';panel.mkdir(parents=True,exist_ok=True)
        (panel/name).write_text(text)
    root=E.Element('channel',name='xfce4-keyboard-shortcuts',version='1.0')
    command_section=property(root,'commands','empty');custom=property(command_section,'custom','empty')
    property(custom,'override','bool','true')
    for key,app in [('<Super>b','browser'),('<Super>Return','terminal'),('<Super>e','files')]:property(custom,key,'string',commands[app])
    property(custom,'<Primary>Escape','string','xfdesktop --menu')
    wm=property(root,'xfwm4','empty');wmcustom=property(wm,'custom','empty');property(wmcustom,'override','bool','true')
    actions={'<Super>d':'show_desktop_key','<Alt>F4':'close_window_key','<Alt>F9':'hide_window_key','<Alt>F10':'maximize_window_key',
        '<Alt>Tab':'cycle_windows_key','<Alt><Shift>Tab':'cycle_reverse_windows_key','<Alt>F12':'fullscreen_key',
        '<Alt>F7':'move_window_key','<Alt>F8':'resize_window_key','<Alt>space':'popup_menu_key',
        'Escape':'cancel_key','Left':'left_key','Right':'right_key','Up':'up_key','Down':'down_key'}
    for key,action in actions.items():property(wmcustom,key,'string',action)
    write_xml(channels/'xfce4-keyboard-shortcuts.xml',root)
    gtk=user/'.config/gtk-3.0';gtk.mkdir(parents=True,exist_ok=True)
    (gtk/'bookmarks').write_text('file://'+home+'/Sidekicks Sidekicks\n')
    autostart=user/'.config/autostart';autostart.mkdir(parents=True,exist_ok=True)
    for name in ['picom','tint2','openbox','xfsettingsd']:
        (autostart/(name+'.desktop')).write_text('[Desktop Entry]\nType=Application\nName=Disabled duplicate '+name+' autostart\nHidden=true\n')

if __name__=='__main__':
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--output',required=True);p.add_argument('--home',required=True)
    p.add_argument('--browser-command',required=True);p.add_argument('--terminal-command',required=True);p.add_argument('--files-command',required=True)
    p.add_argument('--wallpaper',required=True);p.add_argument('--icons',required=True)
    p.add_argument('--monitor',default='screen',help='Actual guest XRandR output name, e.g. screen or DUMMY0')
    p.add_argument('--terminal-wm-class',default='SidekicksTerminal',help='Actual supported terminal WM_CLASS; xfce4-terminal1.0.4 uses Xfce4-terminal plus a managed role')
    a=p.parse_args()
    render(a.output,a.home,{'browser':a.browser_command,'terminal':a.terminal_command,'files':a.files_command},a.wallpaper,a.icons,a.monitor,a.terminal_wm_class)
