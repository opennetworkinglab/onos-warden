//
//  SharedCellsViewController.swift
//  SharedCells
//
//  Created by Thomas Vachuska on 5/13/16.
//  Copyright © 2016 Thomas Vachuska. All rights reserved.
//

import Cocoa

class SharedCellsViewController: NSViewController {

    @IBOutlet weak var tableView: NSTableView!
    
    var cellData: [NSString]? = []
    
    override func viewDidLoad() {
        super.viewDidLoad()
        tableView.setDelegate(self)
        tableView.setDataSource(self)
        tableView.selectionHighlightStyle = .None
    }
    
    func updateCellData(newCellData: NSString) {
        cellData = newCellData.componentsSeparatedByString("\n")
        dispatch_async(dispatch_get_main_queue(), { () -> Void in
            self.tableView.reloadData()
        })
    }
    
}

extension SharedCellsViewController : NSTableViewDataSource {
    func numberOfRowsInTableView(tableView: NSTableView) -> Int {
        return cellData != nil ? cellData!.count : 0
    }
}

extension SharedCellsViewController : NSTableViewDelegate {
    func tableView(tableView: NSTableView, viewForTableColumn tableColumn: NSTableColumn?, row: Int) -> NSView? {
        var cellIdentifier: String = ""
        var text: String = ""
 
        guard let item = cellData?[row] else {
            return nil
        }
        
        let fields = item.componentsSeparatedByString(",")
        
        if tableColumn == tableView.tableColumns[0] {
            text = fields[0]
            cellIdentifier = "userNameID"
        } else if tableColumn == tableView.tableColumns[1] {
            text = fields.count > 1 ? fields[1] : ""
            cellIdentifier = "cellNameID"
        } else if tableColumn == tableView.tableColumns[2] {
            text = fields.count > 2 ? fields[2] : ""
            cellIdentifier = "cellSpecID"
        } else if tableColumn == tableView.tableColumns[3] {
            text = fields.count > 3 ? "\(fields[3]) minutes" : ""
            cellIdentifier = "expirationID"
        }
        
        if let cell = tableView.makeViewWithIdentifier(cellIdentifier, owner: nil) as? NSTableCellView {
            cell.textField?.stringValue = text
            return cell
        }
        return nil
    }
}