I want to create a custom robust database migration in golang that's flexible and scalable. With just like a definition as in prisma. It should be completely custom with two commands Up and Down.
I should be able to define multiple sub commands within those root commands.

Up {
    create table "test" (
        id integer primary key autoincrement,
        name text
    );
    create table "test2" (
        id integer primary key autoincrement,
        name text
    );
    insert into "test" (name) values ('test');
    insert into "test2" (name) values ('test2');
    alter table "test" rename to "test3";
    alter table "test" (
        add column id integer primary key autoincrement,
        drop column name text,
        change column id boolean
    );
}

Down {
    alter table "test" (
        drop column id,
        drop column name
    )
    alter table "test3" rename to "test";
    drop table "test3";
    drop table "test2";
}

Please note this is just a sample, there can be other commands.

So I need you to build the system using lexer, parser, nodes whatever if efficient and make it work. Once parsed, I should be able to create SQL Queries for any Database drivers for both Up and Down commands.
