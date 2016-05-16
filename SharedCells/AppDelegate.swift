//
//  AppDelegate.swift
//  SharedCells
//
//  Created by Thomas Vachuska on 5/13/16.
//  Copyright © 2016 Thomas Vachuska. All rights reserved.
//

import Cocoa

@NSApplicationMain
class AppDelegate: NSObject, NSApplicationDelegate {

    @IBOutlet weak var window: NSWindow!

    let statusItem = NSStatusBar.systemStatusBar().statusItemWithLength(-2)

    let wardenUrl = "http://10.254.1.19:4321/"
    let menu = NSMenu()
    let popover = NSPopover()

    var username = NSUserName()
    var eventMonitor: EventMonitor?
  
    // Start-up hook
    func applicationDidFinishLaunching(aNotification: NSNotification) {
        if let button = statusItem.button {
            button.image = NSImage(named: "Image")
            button.action = #selector(AppDelegate.togglePopover(_:))
        }
        
        popover.contentViewController = SharedCellsViewController(nibName: "SharedCellsViewController", bundle: nil)

        menu.addItem(NSMenuItem(title: "View Cells", action: #selector(viewCells(_:)), keyEquivalent: "s"))
        menu.addItem(NSMenuItem.separatorItem())
        menu.addItem(NSMenuItem(title: "Borrow 1+1 Cell", action: #selector(borrowSmallCell), keyEquivalent: "t"))
        menu.addItem(NSMenuItem(title: "Borrow 3+1 Cell", action: #selector(borrowMediumCell), keyEquivalent: "m"))
        menu.addItem(NSMenuItem(title: "Borrow 5+1 Cell", action: #selector(borrowLargeCell), keyEquivalent: "l"))
        menu.addItem(NSMenuItem.separatorItem())
        menu.addItem(NSMenuItem(title: "Return Cell", action: #selector(returnCell(_:)), keyEquivalent: "r"))
        menu.addItem(NSMenuItem.separatorItem())
        menu.addItem(NSMenuItem(title: "Quit", action: #selector(NSApplication.terminate), keyEquivalent: "q"))
        statusItem.menu = menu
        
        eventMonitor = EventMonitor(mask: .LeftMouseDownMask) { [unowned self] event in
            if self.popover.shown {
                self.closePopover(event)
            }
        }
        eventMonitor?.start()
    }

    // Tear-down hook
    func applicationWillTerminate(aNotification: NSNotification) {
        // Insert code here to tear down your application
    }

    // Obtains data on cell status and displays it in a pop-up window
    func viewCells(sender: AnyObject?) {
        get(wardenUrl, callback: updatePopover)
        showPopover(sender)
    }
    
    func borrowSmallCell() { borrowCell("1+1") }
    func borrowMediumCell() { borrowCell("3+1") }
    func borrowLargeCell() { borrowCell("5+1") }
    
    // Borrow cell for the user and for 60 minutes into the future
    func borrowCell(cellSpec: String) {
        let home = NSHomeDirectory()
        let sshKeyFilePath = home.stringByAppendingString("/.ssh/id_rsa.pub") as String
        let sshKey = try? NSString(contentsOfFile: sshKeyFilePath, encoding: NSUTF8StringEncoding)
        post("\(wardenUrl)?duration=60&user=\(username)", stringData: sshKey! as String, callback: updatePopover)
    }

    // Return cell currently leased by the user
    func returnCell(sender: AnyObject?) {
        delete("\(wardenUrl)?user=\(username)", callback: updatePopover)
    }

    func updatePopover(data: NSString) {
        print(data);
    }
    
    func showPopover(sender: AnyObject?) {
        if let button = statusItem.button {
            popover.showRelativeToRect(button.bounds, ofView: button, preferredEdge: NSRectEdge.MinY)
        }
        eventMonitor?.start()
    }
    
    func closePopover(sender: AnyObject?) {
        popover.performClose(sender)
        eventMonitor?.stop()
    }
    
    func togglePopover(sender: AnyObject?) {
        if popover.shown {
            closePopover(sender)
        } else {
            showPopover(sender)
        }
    }
    
    
    func get(urlPath: String, callback: (NSString) -> Void) {
        let url: NSURL = NSURL(string: urlPath)!
        let request = NSMutableURLRequest(URL: url)
        let task = NSURLSession.sharedSession().dataTaskWithRequest(request) { data, response, error in
            guard error == nil && data != nil else {
                print("error = \(error)")
                return
            }
            if let httpStatus = response as? NSHTTPURLResponse where httpStatus.statusCode != 200 {
                print("status = \(httpStatus.statusCode)")
                print("response = \(response)")
            }
            
            callback(NSString(data: data!, encoding: NSUTF8StringEncoding)!)
        }
        task.resume()
    }
    
    func post(urlPath: String, stringData: String, callback: (NSString) -> Void) {
        let url: NSURL = NSURL(string: urlPath)!
        let request = NSMutableURLRequest(URL: url)
        request.HTTPMethod = "POST"
        
        request.HTTPBody = stringData.dataUsingEncoding(NSUTF8StringEncoding)
        let task = NSURLSession.sharedSession().dataTaskWithRequest(request) { data, response, error in
            guard error == nil && data != nil else {
                print("error = \(error)")
                return
            }
            if let httpStatus = response as? NSHTTPURLResponse where httpStatus.statusCode != 200 {
                print("status = \(httpStatus.statusCode)")
                print("response = \(response)")
            }
            
            callback(NSString(data: data!, encoding: NSUTF8StringEncoding)!)
        }
        task.resume()
    }
    
    func delete(urlPath: String, callback: (NSString) -> Void) {
        let url: NSURL = NSURL(string: urlPath)!
        let request = NSMutableURLRequest(URL: url)
        request.HTTPMethod = "DELETE"
        
        let task = NSURLSession.sharedSession().dataTaskWithRequest(request) { data, response, error in
            guard error == nil && data != nil else {
                print("error = \(error)")
                return
            }
            if let httpStatus = response as? NSHTTPURLResponse where httpStatus.statusCode != 200 {
                print("status = \(httpStatus.statusCode)")
                print("response = \(response)")
            }
            
            callback(NSString(data: data!, encoding: NSUTF8StringEncoding)!)
        }
        task.resume()
    }
}

